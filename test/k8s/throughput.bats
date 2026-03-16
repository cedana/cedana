#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,perf,throughput

# Throughput Efficiency Test for Cedana
#
# This test demonstrates how checkpointing prevents job failures when preemptions
# occur late in job execution, using a wall clock time limit scenario.
#
# Scenario: N jobs with wall clock limits, preempted at regular intervals
# - Baseline: Jobs restart from 0% → jobs preempted late cannot complete → FAIL
# - Cedana: Jobs restore from checkpoint → all jobs complete → SUCCESS
#
# Key metrics:
# - Job completion rate (baseline ~30%, cedana 100%)
# - Number of failed jobs prevented
# - Total wall time saved
#
# Configuration (all via environment variables):
#
# Test Structure:
#   THROUGHPUT_NUM_JOBS              - Number of jobs to run (default: 10)
#   THROUGHPUT_JOB_DURATION          - Job work duration in seconds (default: 2700 = 45 min)
#   THROUGHPUT_WALL_CLOCK_LIMIT      - Wall clock limit per job in seconds (default: 3600 = 60 min)
#   THROUGHPUT_PREEMPT_INTERVAL      - Interval between preemptions in seconds (default: 270 = 4.5 min)
#                                      Jobs are preempted at: 270s, 540s, 810s, 1080s, etc.
#   THROUGHPUT_PREEMPTIONS_PER_JOB   - Number of preemptions per job (default: 1)
#
# Test Execution:
#   THROUGHPUT_WORKLOADS             - Comma-separated workload types (default: monte-carlo-pi)
#   THROUGHPUT_NAMESPACE             - Test namespace (default: throughput-test)
#   THROUGHPUT_JOB_TIMEOUT           - Max time to wait for all jobs (default: 4200 = 70 min)
#   THROUGHPUT_CHECKPOINT_TIMEOUT    - Max time to wait for checkpoint (default: 300 = 5 min)
#   THROUGHPUT_SAMPLES_DIR           - Path to cedana-samples repo (auto-detected)
#
# Advanced:
#   THROUGHPUT_STATE_DIR             - Where to store results (default: /tmp/throughput-test-state)
#   CEDANA_NAMESPACE                 - Cedana system namespace (default: cedana-systems)
#
# Examples:
#
#   # Quick test: 5 jobs, 10 min work, 15 min limit, preempt every 2 min
#   THROUGHPUT_NUM_JOBS=5 THROUGHPUT_JOB_DURATION=600 \
#   THROUGHPUT_WALL_CLOCK_LIMIT=900 THROUGHPUT_PREEMPT_INTERVAL=120 \
#   bats throughput.bats
#
#   # Long test: 20 jobs, 60 min work, 90 min limit, preempt every 3 min
#   THROUGHPUT_NUM_JOBS=20 THROUGHPUT_JOB_DURATION=3600 \
#   THROUGHPUT_WALL_CLOCK_LIMIT=5400 THROUGHPUT_PREEMPT_INTERVAL=180 \
#   bats throughput.bats
#
#   # Multiple preemptions: 10 jobs, preempt twice per job
#   THROUGHPUT_NUM_JOBS=10 THROUGHPUT_PREEMPTIONS_PER_JOB=2 \
#   bats throughput.bats

load ../helpers/utils
load ../helpers/k8s
load ../helpers/propagator

################################################################################
# Configuration
################################################################################

# Test Structure Configuration
THROUGHPUT_NUM_JOBS="${THROUGHPUT_NUM_JOBS:-10}"                      # Number of jobs to run
THROUGHPUT_JOB_DURATION="${THROUGHPUT_JOB_DURATION:-2700}"           # Job work duration (seconds) - default: 45 min
THROUGHPUT_WALL_CLOCK_LIMIT="${THROUGHPUT_WALL_CLOCK_LIMIT:-3600}"  # Wall clock limit per job (seconds) - default: 60 min
THROUGHPUT_PREEMPT_INTERVAL="${THROUGHPUT_PREEMPT_INTERVAL:-270}"   # Preemption interval (seconds) - default: 4.5 min
THROUGHPUT_PREEMPTIONS_PER_JOB="${THROUGHPUT_PREEMPTIONS_PER_JOB:-1}" # Number of times to preempt each job

# Test Execution Configuration
THROUGHPUT_WORKLOADS="${THROUGHPUT_WORKLOADS:-monte-carlo-pi}"      # Workload types
THROUGHPUT_NAMESPACE="${THROUGHPUT_NAMESPACE:-throughput-test}"     # Test namespace
THROUGHPUT_JOB_TIMEOUT="${THROUGHPUT_JOB_TIMEOUT:-4200}"           # Max time to wait for all jobs (70 min)
THROUGHPUT_CHECKPOINT_TIMEOUT="${THROUGHPUT_CHECKPOINT_TIMEOUT:-300}" # Max time to wait for checkpoint (5 min)

# Samples directory - set by setup or environment
THROUGHPUT_SAMPLES_DIR="${THROUGHPUT_SAMPLES_DIR:-}"

# State directory for metrics collection
STATE_DIR="${THROUGHPUT_STATE_DIR:-/tmp/throughput-test-state}"

# Cedana namespace for extracting credentials
CEDANA_NAMESPACE="${CEDANA_NAMESPACE:-cedana-systems}"

# Backward compatibility aliases
SATURATION_NUM_JOBS="${SATURATION_NUM_JOBS:-$THROUGHPUT_NUM_JOBS}"
SATURATION_PREEMPT_INTERVAL="${SATURATION_PREEMPT_INTERVAL:-$THROUGHPUT_PREEMPT_INTERVAL}"
SATURATION_JOB_DURATION="${SATURATION_JOB_DURATION:-$THROUGHPUT_JOB_DURATION}"
SATURATION_WALL_CLOCK_LIMIT="${SATURATION_WALL_CLOCK_LIMIT:-$THROUGHPUT_WALL_CLOCK_LIMIT}"

################################################################################
# Setup / Teardown
################################################################################

setup_file() {
    rm -rf "$STATE_DIR"
    mkdir -p "$STATE_DIR"
    mkdir -p "$STATE_DIR/baseline"
    mkdir -p "$STATE_DIR/cedana"
    mkdir -p "$STATE_DIR/jobs"

    # Find samples directory
    if [[ -z "$THROUGHPUT_SAMPLES_DIR" ]]; then
        # Check local cedana-samples repo first
        if [[ -d "/home/nravic/go/src/github.com/cedana/cedana-samples/kubernetes/jobs" ]]; then
            THROUGHPUT_SAMPLES_DIR="/home/nravic/go/src/github.com/cedana/cedana-samples/kubernetes/jobs"
        elif [[ -d "../cedana-samples/kubernetes/jobs" ]]; then
            THROUGHPUT_SAMPLES_DIR="../cedana-samples/kubernetes/jobs"
        elif [[ -d "/tmp/cedana-samples/kubernetes/jobs" ]]; then
            THROUGHPUT_SAMPLES_DIR="/tmp/cedana-samples/kubernetes/jobs"
        else
            # Clone samples repo
            git clone --depth 1 https://github.com/cedana/cedana-samples.git /tmp/cedana-samples 2>/dev/null || true
            THROUGHPUT_SAMPLES_DIR="/tmp/cedana-samples/kubernetes/jobs"
        fi
    fi
    export THROUGHPUT_SAMPLES_DIR

    # Load cedana credentials from cluster if not already set
    if [[ -z "$CEDANA_URL" ]]; then
        export CEDANA_URL=$(kubectl get cm cedana-config -n "$CEDANA_NAMESPACE" -o jsonpath='{.data.url}' 2>/dev/null || echo "")
    fi
    if [[ -z "$CEDANA_AUTH_TOKEN" ]]; then
        export CEDANA_AUTH_TOKEN=$(kubectl get secret cedana-secrets -n "$CEDANA_NAMESPACE" -o jsonpath='{.data.auth-token}' 2>/dev/null | base64 -d || echo "")
    fi
    if [[ -z "$CLUSTER_ID" ]]; then
        export CLUSTER_ID=$(kubectl get cm cedana-config -n "$CEDANA_NAMESPACE" -o jsonpath='{.data.cluster-id}' 2>/dev/null || echo "")
    fi

    # Normalize URL for propagator helper
    if [[ -n "$CEDANA_URL" ]]; then
        CEDANA_URL="${CEDANA_URL%/}"
        CEDANA_URL="${CEDANA_URL%/v1}"
        if [[ ! "$CEDANA_URL" =~ ^https?:// ]]; then
            CEDANA_URL="https://$CEDANA_URL"
        fi
        export CEDANA_URL
        # Set for propagator.bash
        export PROPAGATOR_BASE_URL="$CEDANA_URL"
        export PROPAGATOR_AUTH_TOKEN="$CEDANA_AUTH_TOKEN"
    fi

    info_log "╔═══════════════════════════════════════════════════════════════╗"
    info_log "║         THROUGHPUT TEST CONFIGURATION                         ║"
    info_log "╠═══════════════════════════════════════════════════════════════╣"
    info_log "║ Test Structure:                                               ║"
    info_log "║   Jobs:              ${THROUGHPUT_NUM_JOBS} concurrent jobs"
    info_log "║   Job duration:      ${THROUGHPUT_JOB_DURATION}s ($((THROUGHPUT_JOB_DURATION/60)) min)"
    info_log "║   Wall clock limit:  ${THROUGHPUT_WALL_CLOCK_LIMIT}s ($((THROUGHPUT_WALL_CLOCK_LIMIT/60)) min)"
    info_log "║   Preempt interval:  ${THROUGHPUT_PREEMPT_INTERVAL}s ($((THROUGHPUT_PREEMPT_INTERVAL/60)) min)"
    info_log "║   Preemptions/job:   ${THROUGHPUT_PREEMPTIONS_PER_JOB}"
    info_log "║                                                               ║"
    info_log "║ Test Execution:                                               ║"
    info_log "║   Workload:          ${THROUGHPUT_WORKLOADS}"
    info_log "║   Namespace:         ${THROUGHPUT_NAMESPACE}"
    info_log "║   Timeout:           ${THROUGHPUT_JOB_TIMEOUT}s ($((THROUGHPUT_JOB_TIMEOUT/60)) min)"
    info_log "║   Samples dir:       ${THROUGHPUT_SAMPLES_DIR}"
    info_log "║   State dir:         ${STATE_DIR}"
    if [[ -n "$CEDANA_URL" ]]; then
    info_log "║                                                               ║"
    info_log "║ Cedana API:                                                   ║"
    info_log "║   URL:               ${CEDANA_URL}"
    info_log "║   Cluster ID:        ${CLUSTER_ID}"
    fi
    info_log "╚═══════════════════════════════════════════════════════════════╝"

    create_namespace "$THROUGHPUT_NAMESPACE"
}

teardown_file() {
    info_log "Cleaning up throughput test..."

    # Generate final reports
    if [[ -d "$STATE_DIR" ]]; then
        generate_json_report
        generate_report
    fi

    # Delete all jobs in namespace
    kubectl delete jobs -n "$THROUGHPUT_NAMESPACE" -l throughput-test=true --wait=false 2>/dev/null || true

    # Wait briefly then force delete namespace
    sleep 5
    delete_namespace "$THROUGHPUT_NAMESPACE" --force --timeout=120s 2>/dev/null || true

    # Clean up temporary files but preserve results.json
    if [[ -d "$STATE_DIR" ]]; then
        rm -rf "$STATE_DIR/baseline" "$STATE_DIR/cedana" "$STATE_DIR/jobs"
        info_log "Test results preserved in: $STATE_DIR/results.json"
    fi
}

################################################################################
# Job Management Helpers
################################################################################

# Generate a unique checkpoint ID
generate_checkpoint_id() {
    cat /proc/sys/kernel/random/uuid 2>/dev/null || uuidgen || echo "ckpt-$(date +%s)-$RANDOM"
}

# Create a job from template, substituting the checkpoint ID
create_job_from_template() {
    local template="$1"
    local job_id="$2"
    local checkpoint_id="$3"

    local output_file="$STATE_DIR/jobs/job-${job_id}.yaml"

    # Copy template and substitute checkpoint ID
    sed "s/PLACEHOLDER_CHECKPOINT_ID/${checkpoint_id}/g" "$template" > "$output_file"

    echo "$output_file"
}

# Submit a job and return its name
submit_job() {
    local spec_file="$1"
    local namespace="$2"

    local output
    output=$(kubectl create -f "$spec_file" -n "$namespace" 2>&1)

    # Extract job name from output like "job.batch/throughput-baseline-monte-carlo-xyz123 created"
    local job_name
    job_name=$(echo "$output" | grep -oP 'job\.batch/\K[^\s]+' | head -1)

    if [[ -z "$job_name" ]]; then
        error_log "Failed to create job: $output"
        return 1
    fi

    echo "$job_name"
}

# Get the pod name for a job (waits for a running pod)
get_job_pod() {
    local job_name="$1"
    local namespace="$2"
    local timeout="${3:-60}"

    local elapsed=0
    while [[ $elapsed -lt $timeout ]]; do
        local pod_name
        # Get running or pending pods first, exclude terminated
        pod_name=$(kubectl get pods -n "$namespace" -l job-name="$job_name" \
            --field-selector=status.phase!=Succeeded,status.phase!=Failed \
            --no-headers -o custom-columns=':metadata.name' 2>/dev/null | head -1)

        if [[ -n "$pod_name" ]]; then
            echo "$pod_name"
            return 0
        fi

        sleep 2
        ((elapsed += 2)) || true
    done

    error_log "Timeout waiting for pod for job $job_name"
    return 1
}

# Get the completed/succeeded pod for a job
get_completed_job_pod() {
    local job_name="$1"
    local namespace="$2"

    kubectl get pods -n "$namespace" -l job-name="$job_name" \
        --field-selector=status.phase=Succeeded \
        --no-headers -o custom-columns=':metadata.name' 2>/dev/null | tail -1
}

# Wait for job to complete (Succeeded or Failed)
wait_for_job_complete() {
    local job_name="$1"
    local namespace="$2"
    local timeout="${3:-600}"

    local elapsed=0
    while [[ $elapsed -lt $timeout ]]; do
        local status
        status=$(kubectl get job "$job_name" -n "$namespace" \
            -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2>/dev/null)

        if [[ "$status" == "True" ]]; then
            return 0
        fi

        local failed
        failed=$(kubectl get job "$job_name" -n "$namespace" \
            -o jsonpath='{.status.conditions[?(@.type=="Failed")].status}' 2>/dev/null)

        if [[ "$failed" == "True" ]]; then
            error_log "Job $job_name failed"
            return 1
        fi

        sleep 5
        ((elapsed += 5))
    done

    error_log "Timeout waiting for job $job_name to complete"
    return 1
}

# Get progress from job pod logs (looks for "PROGRESS:" lines)
get_job_progress() {
    local job_name="$1"
    local namespace="$2"

    local pod_name
    # Try completed pod first, then any pod
    pod_name=$(get_completed_job_pod "$job_name" "$namespace")
    if [[ -z "$pod_name" ]]; then
        pod_name=$(kubectl get pods -n "$namespace" -l job-name="$job_name" \
            --no-headers -o custom-columns=':metadata.name' 2>/dev/null | tail -1)
    fi

    if [[ -z "$pod_name" ]]; then
        echo "PROGRESS: unknown (no pod found)"
        return 0
    fi

    # Get the last PROGRESS line
    kubectl logs "$pod_name" -n "$namespace" 2>/dev/null | \
        grep "PROGRESS:" | tail -1 || echo "PROGRESS: unknown"
}

# Check if job completed successfully (logs contain "COMPLETED")
is_job_completed() {
    local job_name="$1"
    local namespace="$2"

    local pod_name
    pod_name=$(get_job_pod "$job_name" "$namespace" 10) || return 1

    kubectl logs "$pod_name" -n "$namespace" 2>/dev/null | grep -q "COMPLETED"
}

################################################################################
# Metrics Recording
################################################################################

record_metric() {
    local test_mode="$1"  # baseline or cedana
    local workload="$2"
    local metric="$3"
    local value="$4"

    echo "$value" >> "$STATE_DIR/${test_mode}/${workload}_${metric}"
}

# Record job-level data for JSON output
record_job_data() {
    local test_mode="$1"  # baseline or cedana
    local job_id="$2"
    local job_name="$3"
    local start_time="$4"
    local preemption_time="$5"
    local completion_time="$6"
    local status="$7"  # completed, failed, timeout
    local progress_at_preemption="${8:-0}"

    local json_file="$STATE_DIR/${test_mode}/jobs.json"

    # Create JSON array if doesn't exist
    if [[ ! -f "$json_file" ]]; then
        echo "[]" > "$json_file"
    fi

    # Append job data (using jq if available, otherwise simple append)
    local job_json=$(cat <<EOF
{
  "job_id": $job_id,
  "job_name": "$job_name",
  "start_time": $start_time,
  "preemption_time": $preemption_time,
  "completion_time": $completion_time,
  "status": "$status",
  "progress_at_preemption": $progress_at_preemption
}
EOF
)

    # Simple JSON append (no jq dependency)
    if command -v jq &> /dev/null; then
        jq ". += [$job_json]" "$json_file" > "${json_file}.tmp" && mv "${json_file}.tmp" "$json_file"
    else
        # Manual JSON array append
        if [[ $(wc -l < "$json_file") -eq 1 ]]; then
            # Empty array
            echo "[$job_json]" > "$json_file"
        else
            # Append to array (remove trailing ], add comma, add new entry, close])
            sed -i '$ d' "$json_file"  # Remove last ]
            echo ",$job_json]" >> "$json_file"
        fi
    fi
}

get_metric_sum() {
    local test_mode="$1"
    local workload="$2"
    local metric="$3"

    local file="$STATE_DIR/${test_mode}/${workload}_${metric}"
    if [[ -f "$file" ]]; then
        awk '{sum+=$1} END {print sum+0}' "$file"
    else
        echo "0"
    fi
}

get_preemption_times_summary() {
    local test_mode="$1"
    local workload="$2"
    local metric="${3:-preemption_times}"  # Default to preemption_times for backwards compatibility

    local file="$STATE_DIR/${test_mode}/${workload}_${metric}"
    if [[ -f "$file" ]]; then
        # Calculate min, max, avg of preemption times
        awk 'BEGIN {min=999999; max=0; sum=0; count=0}
             {
                 if ($1 < min) min=$1;
                 if ($1 > max) max=$1;
                 sum+=$1;
                 count++
             }
             END {
                 if (count > 0) {
                     printf "min=%ds, max=%ds, avg=%ds", min, max, sum/count
                 } else {
                     print "no data"
                 }
             }' "$file"
    else
        echo "no data"
    fi
}

get_preemption_times_list() {
    local test_mode="$1"
    local workload="$2"
    local metric="${3:-preemption_times}"  # Default to preemption_times for backwards compatibility

    local file="$STATE_DIR/${test_mode}/${workload}_${metric}"
    if [[ -f "$file" ]]; then
        tr '\n' ',' < "$file" | sed 's/,$//' | sed 's/,/, /g' | sed 's/\([0-9]\+\)/\1s/g'
    else
        echo "none"
    fi
}

generate_json_report() {
    local output_file="$STATE_DIR/results.json"

    info_log "Generating JSON report: $output_file"

    # Get baseline data
    local baseline_jobs_json="[]"
    if [[ -f "$STATE_DIR/baseline/jobs.json" ]]; then
        baseline_jobs_json=$(cat "$STATE_DIR/baseline/jobs.json")
    fi

    local baseline_wall=$(get_metric_sum "baseline" "monte-carlo-pi" "saturation_wall_time")
    local baseline_completed=$(get_metric_sum "baseline" "monte-carlo-pi" "saturation_completed")
    local baseline_failed=$(get_metric_sum "baseline" "monte-carlo-pi" "saturation_failed")
    local baseline_jobs=$(get_metric_sum "baseline" "monte-carlo-pi" "saturation_jobs")

    # Get cedana data
    local cedana_jobs_json="[]"
    if [[ -f "$STATE_DIR/cedana/jobs.json" ]]; then
        cedana_jobs_json=$(cat "$STATE_DIR/cedana/jobs.json")
    fi

    local cedana_wall=$(get_metric_sum "cedana" "monte-carlo-pi" "saturation_wall_time")
    local cedana_completed=$(get_metric_sum "cedana" "monte-carlo-pi" "saturation_completed")
    local cedana_failed=$(get_metric_sum "cedana" "monte-carlo-pi" "saturation_failed")
    local cedana_jobs=$(get_metric_sum "cedana" "monte-carlo-pi" "saturation_jobs")

    # Calculate throughput metrics
    local baseline_throughput=$(awk "BEGIN {if (${baseline_wall} > 0) print ${baseline_completed}/${baseline_wall}; else print 0}")
    local cedana_throughput=$(awk "BEGIN {if (${cedana_wall} > 0) print ${cedana_completed}/${cedana_wall}; else print 0}")
    local throughput_improvement=$(awk "BEGIN {if (${baseline_throughput} > 0) print ((${cedana_throughput} - ${baseline_throughput}) / ${baseline_throughput}) * 100; else print 0}")
    local time_reduction=$(awk "BEGIN {if (${baseline_wall} > 0) print ((${baseline_wall} - ${cedana_wall}) / ${baseline_wall}) * 100; else print 0}")

    # Create JSON report
    cat > "$output_file" <<EOF
{
  "test_config": {
    "num_jobs": ${THROUGHPUT_NUM_JOBS},
    "job_duration_sec": ${THROUGHPUT_JOB_DURATION},
    "wall_clock_limit_sec": ${THROUGHPUT_WALL_CLOCK_LIMIT},
    "preemption_interval_sec": ${THROUGHPUT_PREEMPT_INTERVAL},
    "preemptions_per_job": ${THROUGHPUT_PREEMPTIONS_PER_JOB},
    "workload": "${THROUGHPUT_WORKLOADS}",
    "namespace": "${THROUGHPUT_NAMESPACE}"
  },
  "baseline": {
    "total_wall_time_sec": ${baseline_wall},
    "jobs": ${baseline_jobs_json},
    "summary": {
      "total": ${baseline_jobs},
      "completed": ${baseline_completed},
      "failed": ${baseline_failed},
      "completion_rate": $(awk "BEGIN {if (${baseline_jobs} > 0) print ${baseline_completed}/${baseline_jobs}; else print 0}"),
      "throughput": ${baseline_throughput}
    }
  },
  "cedana": {
    "total_wall_time_sec": ${cedana_wall},
    "jobs": ${cedana_jobs_json},
    "summary": {
      "total": ${cedana_jobs},
      "completed": ${cedana_completed},
      "failed": ${cedana_failed},
      "completion_rate": $(awk "BEGIN {if (${cedana_jobs} > 0) print ${cedana_completed}/${cedana_jobs}; else print 0}"),
      "throughput": ${cedana_throughput}
    }
  },
  "performance": {
    "time_reduction_percent": ${time_reduction},
    "throughput_improvement_percent": ${throughput_improvement},
    "additional_jobs_completed": $((cedana_completed - baseline_completed)),
    "failures_prevented": ${baseline_failed}
  }
}
EOF

    info_log "JSON report saved: $output_file"
}

generate_report() {
    echo ""
    echo "╔════════════════════════════════════════════════════════════════════════╗"
    echo "║           CEDANA THROUGHPUT EFFICIENCY REPORT                          ║"
    echo "║  Demonstrates how checkpointing prevents throughput degradation        ║"
    echo "║  when jobs are preempted in resource-constrained clusters              ║"
    echo "╠════════════════════════════════════════════════════════════════════════╣"
    echo "║  Test scenario: N concurrent jobs competing for limited resources      ║"
    echo "║  • Jobs queue when nodes are full                                      ║"
    echo "║  • Running jobs get preempted (spot reclamation, etc.)                 ║"
    echo "║  • Baseline: Restart from scratch → queue backs up                     ║"
    echo "║  • Cedana: Restore from checkpoint → queue clears faster               ║"
    echo "╠════════════════════════════════════════════════════════════════════════╣"

    IFS=',' read -ra workloads <<< "$THROUGHPUT_WORKLOADS"

    for workload in "${workloads[@]}"; do
        # Get test metrics
        local baseline_wall=$(get_metric_sum "baseline" "$workload" "saturation_wall_time")
        local baseline_jobs=$(get_metric_sum "baseline" "$workload" "saturation_jobs")
        local baseline_completed=$(get_metric_sum "baseline" "$workload" "saturation_completed")
        local baseline_preempt_summary=$(get_preemption_times_summary "baseline" "$workload" "saturation_preemption_times")
        local baseline_preempt_list=$(get_preemption_times_list "baseline" "$workload" "saturation_preemption_times")

        local cedana_wall=$(get_metric_sum "cedana" "$workload" "saturation_wall_time")
        local cedana_jobs=$(get_metric_sum "cedana" "$workload" "saturation_jobs")
        local cedana_completed=$(get_metric_sum "cedana" "$workload" "saturation_completed")
        local cedana_preempt_summary=$(get_preemption_times_summary "cedana" "$workload" "saturation_preemption_times")
        local cedana_preempt_list=$(get_preemption_times_list "cedana" "$workload" "saturation_preemption_times")

        # Skip if no data
        if [[ $baseline_wall -eq 0 ]] && [[ $cedana_wall -eq 0 ]]; then
            continue
        fi

        local time_saved=$((baseline_wall - cedana_wall))
        local efficiency=0
        if [[ $baseline_wall -gt 0 ]]; then
            efficiency=$((time_saved * 100 / baseline_wall))
        fi

        echo "║"
        echo "║ Workload: $workload ($baseline_jobs concurrent jobs)"
        echo "║   Baseline (no checkpointing): ${baseline_wall}s  ($baseline_completed/$baseline_jobs completed)"
        if [[ "$baseline_preempt_list" != "none" ]]; then
            echo "║     Preemption times: [$baseline_preempt_list]"
            echo "║     Summary: $baseline_preempt_summary"
        fi
        echo "║   Cedana (checkpoint/restore): ${cedana_wall}s  ($cedana_completed/$cedana_jobs completed)"
        if [[ "$cedana_preempt_list" != "none" ]]; then
            echo "║     Preemption times: [$cedana_preempt_list]"
            echo "║     Summary: $cedana_preempt_summary"
        fi
        echo "║   ─────────────────────────────────────────────────────────────────"
        echo "║   Time saved: ${time_saved}s  |  Throughput improvement: ${efficiency}%"
        echo "║"
    done

    echo "╚════════════════════════════════════════════════════════════════════════╝"
    echo ""
}

################################################################################
# Throughput Test - Submit many jobs concurrently, measure total throughput
################################################################################

# Generate evenly spaced preemption times for jobs
# With 1 preemption per job: Job 1 at 270s, Job 2 at 540s, etc.
# With 2 preemptions per job: Job 1 at 270s and 540s, Job 2 at 810s and 1080s, etc.
get_preemption_times() {
    local job_index="$1"  # 0-based
    local num_preemptions="${THROUGHPUT_PREEMPTIONS_PER_JOB}"
    local interval="${THROUGHPUT_PREEMPT_INTERVAL}"

    # Calculate base offset for this job
    local base_offset=$((job_index * num_preemptions))

    # Generate preemption times for this job
    local times=""
    for i in $(seq 1 "$num_preemptions"); do
        local preempt_time=$((interval * (base_offset + i)))
        if [[ -z "$times" ]]; then
            times="$preempt_time"
        else
            times="$times $preempt_time"
        fi
    done

    echo "$times"
}

# Get the first preemption time for a job (for backward compatibility)
get_preemption_time() {
    local job_index="$1"  # 0-based
    local times=$(get_preemption_times "$job_index")
    echo "${times%% *}"  # Return first time
}

# Run throughput baseline test - all jobs submitted at once, random preemptions
run_throughput_baseline() {
    local workload="$1"
    local num_jobs="${2:-$SATURATION_NUM_JOBS}"

    local template_file="$THROUGHPUT_SAMPLES_DIR/${workload}-baseline.yaml"

    if [[ ! -f "$template_file" ]]; then
        error_log "Template not found: $template_file"
        return 1
    fi

    info_log "[throughput-baseline] Submitting $num_jobs jobs for workload: $workload"

    local start_time
    start_time=$(date +%s)

    # Arrays to track jobs
    declare -a job_names
    declare -a pod_names
    declare -a preempt_delays
    declare -a preempted

    # Submit all jobs at once with evenly spaced preemption schedule
    for i in $(seq 0 $((num_jobs - 1))); do
        local job_name
        job_name=$(submit_job "$template_file" "$THROUGHPUT_NAMESPACE")
        if [[ -n "$job_name" ]]; then
            job_names+=("$job_name")
            local preempt_time=$(get_preemption_time "$i")
            preempt_delays+=("$preempt_time")
            preempted+=("false")
            info_log "[throughput-baseline] Job $((i+1)): submitted as $job_name (preempt at ${preempt_time}s = $((preempt_time/60)) min)"
        fi
    done

    local submitted_time
    submitted_time=$(date +%s)
    info_log "[throughput-baseline] All $num_jobs jobs submitted in $((submitted_time - start_time))s"

    # Wait for all pods to start running
    info_log "[throughput-baseline] Waiting for pods to start..."
    for i in "${!job_names[@]}"; do
        local pod_name
        pod_name=$(get_job_pod "${job_names[$i]}" "$THROUGHPUT_NAMESPACE" 180) || true
        pod_names+=("$pod_name")
        if [[ -n "$pod_name" ]]; then
            info_log "[throughput-baseline] Job $((i+1)): pod $pod_name"
        fi
    done

    # Monitor and preempt jobs at their random times
    info_log "[throughput-baseline] Monitoring for preemptions..."
    local all_preempted=false
    while [[ "$all_preempted" == "false" ]]; do
        all_preempted=true
        local elapsed=$(($(date +%s) - submitted_time))

        for i in "${!job_names[@]}"; do
            if [[ "${preempted[$i]}" == "false" ]] && [[ -n "${pod_names[$i]}" ]]; then
                if [[ $elapsed -ge ${preempt_delays[$i]} ]]; then
                    local preempt_timestamp=$(($(date +%s) - start_time))
                    info_log "[throughput-baseline] Job $((i+1)): PREEMPTING at ${elapsed}s (absolute: ${preempt_timestamp}s from start, pod ${pod_names[$i]})"
                    kubectl delete pod "${pod_names[$i]}" -n "$THROUGHPUT_NAMESPACE" --wait=false 2>/dev/null || true
                    preempted[$i]="true"
                    # Record preemption time
                    record_metric "baseline" "$workload" "saturation_preemption_times" "$preempt_timestamp"
                else
                    all_preempted=false
                fi
            fi
        done

        if [[ "$all_preempted" == "false" ]]; then
            sleep 2
        fi
    done

    info_log "[throughput-baseline] All jobs preempted, waiting for completion..."

    # Wait for all jobs to complete or fail
    local completed=0
    local failed=0
    local timeout=$((THROUGHPUT_JOB_TIMEOUT + 60))
    local wait_start
    wait_start=$(date +%s)

    # Track completion status per job
    declare -a job_completed
    declare -a job_completion_times
    for i in "${!job_names[@]}"; do
        job_completed+=("false")
        job_completion_times+=(0)
    done

    while [[ $((completed + failed)) -lt ${#job_names[@]} ]]; do
        for i in "${!job_names[@]}"; do
            if [[ "${job_completed[$i]}" == "false" ]]; then
                local job_name="${job_names[$i]}"

                # Check if succeeded
                local success_status
                success_status=$(kubectl get job "$job_name" -n "$THROUGHPUT_NAMESPACE" \
                    -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2>/dev/null)

                # Check if failed (including DeadlineExceeded)
                local failed_status
                failed_status=$(kubectl get job "$job_name" -n "$THROUGHPUT_NAMESPACE" \
                    -o jsonpath='{.status.conditions[?(@.type=="Failed")].status}' 2>/dev/null)

                if [[ "$success_status" == "True" ]]; then
                    job_completed[$i]="completed"
                    job_completion_times[$i]=$(($(date +%s) - start_time))
                    ((completed++)) || true
                    info_log "[throughput-baseline] Job $((i+1)) COMPLETED at ${job_completion_times[$i]}s"
                elif [[ "$failed_status" == "True" ]]; then
                    job_completed[$i]="failed"
                    job_completion_times[$i]=$(($(date +%s) - start_time))
                    ((failed++)) || true
                    info_log "[throughput-baseline] Job $((i+1)) FAILED at ${job_completion_times[$i]}s"
                fi
            fi
        done

        local wait_elapsed=$(($(date +%s) - wait_start))
        if [[ $wait_elapsed -gt $timeout ]]; then
            error_log "[throughput-baseline] Timeout waiting for jobs ($completed completed, $failed failed, $((${#job_names[@]} - completed - failed)) unknown)"
            break
        fi

        if [[ $((completed + failed)) -lt ${#job_names[@]} ]]; then
            sleep 5
        fi
    done

    local end_time
    end_time=$(date +%s)
    local total_wall_time=$((end_time - start_time))

    info_log "[throughput-baseline] Complete: $completed completed, $failed failed, total time ${total_wall_time}s"

    # Record per-job data to JSON
    for i in "${!job_names[@]}"; do
        local job_status="${job_completed[$i]}"
        if [[ "$job_status" == "false" ]]; then
            job_status="timeout"
        fi

        # Calculate progress at preemption (time elapsed / job duration * 100)
        local progress_pct=$(( (preempt_delays[$i] * 100) / SATURATION_JOB_DURATION ))

        record_job_data "baseline" "$((i+1))" "${job_names[$i]}" \
            "$start_time" "${preempt_delays[$i]}" "${job_completion_times[$i]}" \
            "$job_status" "$progress_pct"
    done

    # Record aggregate metrics
    record_metric "baseline" "$workload" "saturation_wall_time" "$total_wall_time"
    record_metric "baseline" "$workload" "saturation_jobs" "$num_jobs"
    record_metric "baseline" "$workload" "saturation_completed" "$completed"
    record_metric "baseline" "$workload" "saturation_failed" "$failed"

    # Cleanup
    for job_name in "${job_names[@]}"; do
        kubectl delete job "$job_name" -n "$THROUGHPUT_NAMESPACE" --wait=false 2>/dev/null || true
    done

    echo "$total_wall_time"
}

# Run throughput cedana test - all jobs submitted at once, checkpoint/restore on random preemptions
run_throughput_cedana() {
    local workload="$1"
    local num_jobs="${2:-$SATURATION_NUM_JOBS}"

    local template_file="$THROUGHPUT_SAMPLES_DIR/${workload}-cedana.yaml"

    if [[ ! -f "$template_file" ]]; then
        error_log "Template not found: $template_file"
        return 1
    fi

    # Verify API credentials
    if [[ -z "$CEDANA_URL" ]] || [[ -z "$CEDANA_AUTH_TOKEN" ]]; then
        error_log "Cedana API credentials not configured"
        return 1
    fi

    info_log "[throughput-cedana] Submitting $num_jobs jobs for workload: $workload"

    local start_time
    start_time=$(date +%s)

    # Arrays to track jobs
    declare -a job_names
    declare -a pod_names
    declare -a pod_uids
    declare -a preempt_delays
    declare -a preempted
    declare -a action_ids

    # Submit all jobs at once with evenly spaced preemption schedule
    for i in $(seq 0 $((num_jobs - 1))); do
        local checkpoint_id
        checkpoint_id=$(generate_checkpoint_id)

        local job_spec
        job_spec=$(create_job_from_template "$template_file" "sat-cedana-${workload}-${i}" "$checkpoint_id")

        local job_name
        job_name=$(submit_job "$job_spec" "$THROUGHPUT_NAMESPACE")
        if [[ -n "$job_name" ]]; then
            job_names+=("$job_name")
            local preempt_time=$(get_preemption_time "$i")
            preempt_delays+=("$preempt_time")
            preempted+=("false")
            action_ids+=("")
            info_log "[throughput-cedana] Job $((i+1)): submitted as $job_name (preempt at ${preempt_time}s = $((preempt_time/60)) min)"
        fi
    done

    local submitted_time
    submitted_time=$(date +%s)
    info_log "[throughput-cedana] All $num_jobs jobs submitted in $((submitted_time - start_time))s"

    # Wait for all pods to start running and get UIDs
    info_log "[throughput-cedana] Waiting for pods to start..."
    for i in "${!job_names[@]}"; do
        local pod_name
        pod_name=$(get_job_pod "${job_names[$i]}" "$THROUGHPUT_NAMESPACE" 180) || true
        pod_names+=("$pod_name")
        if [[ -n "$pod_name" ]]; then
            local pod_uid
            pod_uid=$(kubectl get pod "$pod_name" -n "$THROUGHPUT_NAMESPACE" -o jsonpath='{.metadata.uid}' 2>/dev/null)
            pod_uids+=("$pod_uid")
            info_log "[throughput-cedana] Job $((i+1)): pod $pod_name (uid: $pod_uid)"
        else
            pod_uids+=("")
        fi
    done

    # Monitor and checkpoint/preempt/restore jobs at their random times
    # Use non-blocking approach: trigger checkpoint, brief wait, delete, restore
    info_log "[throughput-cedana] Monitoring for preemptions..."
    local all_preempted=false
    while [[ "$all_preempted" == "false" ]]; do
        all_preempted=true
        local elapsed=$(($(date +%s) - submitted_time))

        for i in "${!job_names[@]}"; do
            if [[ "${preempted[$i]}" == "false" ]] && [[ -n "${pod_names[$i]}" ]] && [[ -n "${pod_uids[$i]}" ]]; then
                if [[ $elapsed -ge ${preempt_delays[$i]} ]]; then
                    local job_num=$((i+1))
                    local preempt_timestamp=$(($(date +%s) - start_time))
                    info_log "[throughput-cedana] Job $job_num: CHECKPOINTING at ${elapsed}s (absolute: ${preempt_timestamp}s from start)"

                    # Checkpoint (non-blocking - just trigger it)
                    local action_id
                    action_id=$(checkpoint_pod "${pod_uids[$i]}" "/run/containerd/runc/k8s.io") || true
                    action_ids[$i]="$action_id"

                    if [[ -n "$action_id" ]]; then
                        # Brief wait for checkpoint to initialize (not full completion)
                        sleep 3

                        # Delete pod
                        info_log "[throughput-cedana] Job $job_num: PREEMPTING"
                        kubectl delete pod "${pod_names[$i]}" -n "$THROUGHPUT_NAMESPACE" --wait=false 2>/dev/null || true

                        # Restore immediately (propagator handles checkpoint completion internally)
                        info_log "[throughput-cedana] Job $job_num: RESTORING"
                        restore_pod "$action_id" "$CLUSTER_ID" || true
                    else
                        # Checkpoint failed, just delete pod (baseline behavior)
                        kubectl delete pod "${pod_names[$i]}" -n "$THROUGHPUT_NAMESPACE" --wait=false 2>/dev/null || true
                    fi

                    # Record preemption time
                    record_metric "cedana" "$workload" "saturation_preemption_times" "$preempt_timestamp"
                    preempted[$i]="true"
                else
                    all_preempted=false
                fi
            fi
        done

        if [[ "$all_preempted" == "false" ]]; then
            sleep 2
        fi
    done

    info_log "[throughput-cedana] All jobs checkpointed/preempted/restored, waiting for completion..."

    # Wait for all jobs to complete or fail
    local completed=0
    local failed=0
    local timeout=$((THROUGHPUT_JOB_TIMEOUT + 60))
    local wait_start
    wait_start=$(date +%s)

    # Track completion status per job
    declare -a job_completed
    declare -a job_completion_times
    for i in "${!job_names[@]}"; do
        job_completed+=("false")
        job_completion_times+=(0)
    done

    while [[ $((completed + failed)) -lt ${#job_names[@]} ]]; do
        for i in "${!job_names[@]}"; do
            if [[ "${job_completed[$i]}" == "false" ]]; then
                local job_name="${job_names[$i]}"

                # Check if succeeded
                local success_status
                success_status=$(kubectl get job "$job_name" -n "$THROUGHPUT_NAMESPACE" \
                    -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2>/dev/null)

                # Check if failed
                local failed_status
                failed_status=$(kubectl get job "$job_name" -n "$THROUGHPUT_NAMESPACE" \
                    -o jsonpath='{.status.conditions[?(@.type=="Failed")].status}' 2>/dev/null)

                if [[ "$success_status" == "True" ]]; then
                    job_completed[$i]="completed"
                    job_completion_times[$i]=$(($(date +%s) - start_time))
                    ((completed++)) || true
                    info_log "[throughput-cedana] Job $((i+1)) COMPLETED at ${job_completion_times[$i]}s"
                elif [[ "$failed_status" == "True" ]]; then
                    job_completed[$i]="failed"
                    job_completion_times[$i]=$(($(date +%s) - start_time))
                    ((failed++)) || true
                    info_log "[throughput-cedana] Job $((i+1)) FAILED at ${job_completion_times[$i]}s"
                fi
            fi
        done

        local wait_elapsed=$(($(date +%s) - wait_start))
        if [[ $wait_elapsed -gt $timeout ]]; then
            error_log "[throughput-cedana] Timeout waiting for jobs ($completed completed, $failed failed, $((${#job_names[@]} - completed - failed)) unknown)"
            break
        fi

        if [[ $((completed + failed)) -lt ${#job_names[@]} ]]; then
            sleep 5
        fi
    done

    local end_time
    end_time=$(date +%s)
    local total_wall_time=$((end_time - start_time))

    info_log "[throughput-cedana] Complete: $completed completed, $failed failed, total time ${total_wall_time}s"

    # Record per-job data to JSON
    for i in "${!job_names[@]}"; do
        local job_status="${job_completed[$i]}"
        if [[ "$job_status" == "false" ]]; then
            job_status="timeout"
        fi

        # Calculate progress at preemption
        local progress_pct=$(( (preempt_delays[$i] * 100) / SATURATION_JOB_DURATION ))

        record_job_data "cedana" "$((i+1))" "${job_names[$i]}" \
            "$start_time" "${preempt_delays[$i]}" "${job_completion_times[$i]}" \
            "$job_status" "$progress_pct"
    done

    # Record aggregate metrics
    record_metric "cedana" "$workload" "saturation_wall_time" "$total_wall_time"
    record_metric "cedana" "$workload" "saturation_jobs" "$num_jobs"
    record_metric "cedana" "$workload" "saturation_completed" "$completed"
    record_metric "cedana" "$workload" "saturation_failed" "$failed"

    # Cleanup
    for job_name in "${job_names[@]}"; do
        kubectl delete job "$job_name" -n "$THROUGHPUT_NAMESPACE" --wait=false 2>/dev/null || true
    done

    echo "$total_wall_time"
}

################################################################################
# Tests
################################################################################

# bats test_tags=throughput,baseline,monte-carlo
@test "Throughput: Baseline (monte-carlo-pi) - concurrent jobs, no checkpointing" {
    [[ -f "$THROUGHPUT_SAMPLES_DIR/monte-carlo-pi-baseline.yaml" ]] || skip "Sample not found"
    run_throughput_baseline "monte-carlo-pi" "${SATURATION_NUM_JOBS:-10}"
}

# bats test_tags=throughput,cedana,monte-carlo
@test "Throughput: Cedana (monte-carlo-pi) - concurrent jobs with checkpoint/restore" {
    [[ -f "$THROUGHPUT_SAMPLES_DIR/monte-carlo-pi-cedana.yaml" ]] || skip "Sample not found"
    run_throughput_cedana "monte-carlo-pi" "${SATURATION_NUM_JOBS:-10}"
}
