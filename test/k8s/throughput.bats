#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,perf,throughput

# Throughput Efficiency Test for Cedana
#
# This test demonstrates how checkpointing prevents throughput degradation when
# jobs are preempted in resource-constrained clusters.
#
# Scenario: N concurrent jobs competing for M nodes (N > M)
# - Jobs queue when nodes are full
# - Running jobs get preempted (spot reclamation, priority preemption, etc.)
# - Baseline: Jobs restart from scratch → take longer → queue stays backed up
# - Cedana: Jobs restore from checkpoint → complete faster → queue clears faster
#
# Key metric: Total wall time from job submission to all jobs complete
# This captures the queue buildup effect and overall cluster throughput.
#
# Preemption scenarios covered:
# - Spot/preemptible instance reclamation
# - Priority-based preemption
# - Node draining for maintenance
# - Resource pressure evictions
#
# For cedana jobs, we use the propagator API to:
# 1. Trigger checkpoint before preemption
# 2. Delete pod to simulate preemption
# 3. Trigger restore via API
#
# Environment variables:
#   SATURATION_NUM_JOBS        - Number of concurrent jobs (default: 10)
#   SATURATION_MIN_DELAY       - Min seconds before preemption (default: 60)
#   SATURATION_MAX_DELAY       - Max seconds before preemption (default: 180)
#   THROUGHPUT_WORKLOADS       - Comma-separated workload types (default: monte-carlo-pi)
#   THROUGHPUT_NAMESPACE       - Test namespace (default: throughput-test)
#   THROUGHPUT_JOB_TIMEOUT     - Max seconds to wait for job completion (default: 1800)
#
# Test design:
#   - Submit N concurrent jobs competing for limited cluster resources
#   - Jobs that can't get resources wait in queue (pending)
#   - Preempt running jobs at random times
#   - Baseline: Jobs restart from scratch → longer completion → queue backs up
#   - Cedana: Jobs restore from checkpoint → faster completion → queue clears faster
#   - Measure total wall time to demonstrate throughput impact
#
# Resource configuration:
#   - To simulate "10 jobs on 5 nodes" (queue buildup), configure job YAML with
#     resource requests matching full node capacity (e.g., all CPU/memory/GPU)
#   - Kubernetes scheduler naturally creates queue when requests exceed capacity

load ../helpers/utils
load ../helpers/k8s
load ../helpers/propagator

################################################################################
# Configuration
################################################################################

THROUGHPUT_WORKLOADS="${THROUGHPUT_WORKLOADS:-monte-carlo-pi}"
THROUGHPUT_NAMESPACE="${THROUGHPUT_NAMESPACE:-throughput-test}"
THROUGHPUT_JOB_TIMEOUT="${THROUGHPUT_JOB_TIMEOUT:-4200}"  # Max time to wait for job completion (70 min - safety margin over 60 min)
THROUGHPUT_CHECKPOINT_TIMEOUT="${THROUGHPUT_CHECKPOINT_TIMEOUT:-300}"  # Max time to wait for checkpoint (5 min for large checkpoints)

# Samples directory - set by setup or environment
THROUGHPUT_SAMPLES_DIR="${THROUGHPUT_SAMPLES_DIR:-}"

# State directory for metrics collection (fixed name so it persists across tests in a run)
STATE_DIR="/tmp/throughput-test-state"

# Cedana namespace for extracting credentials
CEDANA_NAMESPACE="${CEDANA_NAMESPACE:-cedana-systems}"

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

    info_log "Using samples from: $THROUGHPUT_SAMPLES_DIR"
    info_log "Workloads: $THROUGHPUT_WORKLOADS"
    info_log "Jobs per workload: $THROUGHPUT_NUM_JOBS"
    info_log "Preemption delay: ${THROUGHPUT_INTERRUPT_DELAY}s"
    if [[ -n "$CEDANA_URL" ]]; then
        info_log "Cedana API: $CEDANA_URL"
        info_log "Cluster ID: $CLUSTER_ID"
    fi

    create_namespace "$THROUGHPUT_NAMESPACE"
}

teardown_file() {
    info_log "Cleaning up throughput test..."

    # Generate final report
    if [[ -d "$STATE_DIR" ]]; then
        generate_report
    fi

    # Delete all jobs in namespace
    kubectl delete jobs -n "$THROUGHPUT_NAMESPACE" -l throughput-test=true --wait=false 2>/dev/null || true

    # Wait briefly then force delete namespace
    sleep 5
    delete_namespace "$THROUGHPUT_NAMESPACE" --force --timeout=120s 2>/dev/null || true

    rm -rf "$STATE_DIR"
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

# Test configuration
SATURATION_NUM_JOBS="${SATURATION_NUM_JOBS:-10}"      # Number of concurrent jobs to run
SATURATION_MIN_DELAY="${SATURATION_MIN_DELAY:-1200}"  # Min seconds before preemption (~20 min, halfway through 45-min job)
SATURATION_MAX_DELAY="${SATURATION_MAX_DELAY:-1500}"  # Max seconds before preemption (~25 min, halfway through 45-min job)

# Generate random delay between min and max
random_delay() {
    local min="$1"
    local max="$2"
    echo $((min + RANDOM % (max - min + 1)))
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

    # Submit all jobs at once
    for i in $(seq 1 "$num_jobs"); do
        local job_name
        job_name=$(submit_job "$template_file" "$THROUGHPUT_NAMESPACE")
        if [[ -n "$job_name" ]]; then
            job_names+=("$job_name")
            preempt_delays+=("$(random_delay "$SATURATION_MIN_DELAY" "$SATURATION_MAX_DELAY")")
            preempted+=("false")
            info_log "[throughput-baseline] Job $i: submitted as $job_name (preempt at ${preempt_delays[-1]}s)"
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

    # Wait for all jobs to complete
    local completed=0
    local timeout=$((THROUGHPUT_JOB_TIMEOUT + 60))
    local wait_start
    wait_start=$(date +%s)

    while [[ $completed -lt ${#job_names[@]} ]]; do
        completed=0
        for job_name in "${job_names[@]}"; do
            local status
            status=$(kubectl get job "$job_name" -n "$THROUGHPUT_NAMESPACE" \
                -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2>/dev/null)
            if [[ "$status" == "True" ]]; then
                ((completed++)) || true
            fi
        done

        local wait_elapsed=$(($(date +%s) - wait_start))
        if [[ $wait_elapsed -gt $timeout ]]; then
            error_log "[throughput-baseline] Timeout waiting for jobs to complete ($completed/${#job_names[@]} done)"
            break
        fi

        if [[ $completed -lt ${#job_names[@]} ]]; then
            sleep 5
        fi
    done

    local end_time
    end_time=$(date +%s)
    local total_wall_time=$((end_time - start_time))

    info_log "[throughput-baseline] Complete: $completed/${#job_names[@]} jobs in ${total_wall_time}s"

    # Record metrics
    record_metric "baseline" "$workload" "saturation_wall_time" "$total_wall_time"
    record_metric "baseline" "$workload" "saturation_jobs" "$num_jobs"
    record_metric "baseline" "$workload" "saturation_completed" "$completed"

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

    # Submit all jobs at once
    for i in $(seq 1 "$num_jobs"); do
        local checkpoint_id
        checkpoint_id=$(generate_checkpoint_id)

        local job_spec
        job_spec=$(create_job_from_template "$template_file" "sat-cedana-${workload}-${i}" "$checkpoint_id")

        local job_name
        job_name=$(submit_job "$job_spec" "$THROUGHPUT_NAMESPACE")
        if [[ -n "$job_name" ]]; then
            job_names+=("$job_name")
            preempt_delays+=("$(random_delay "$SATURATION_MIN_DELAY" "$SATURATION_MAX_DELAY")")
            preempted+=("false")
            action_ids+=("")
            info_log "[throughput-cedana] Job $i: submitted as $job_name (preempt at ${preempt_delays[-1]}s)"
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

    # Wait for all jobs to complete
    local completed=0
    local timeout=$((THROUGHPUT_JOB_TIMEOUT + 60))
    local wait_start
    wait_start=$(date +%s)

    while [[ $completed -lt ${#job_names[@]} ]]; do
        completed=0
        for job_name in "${job_names[@]}"; do
            local status
            status=$(kubectl get job "$job_name" -n "$THROUGHPUT_NAMESPACE" \
                -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2>/dev/null)
            if [[ "$status" == "True" ]]; then
                ((completed++)) || true
            fi
        done

        local wait_elapsed=$(($(date +%s) - wait_start))
        if [[ $wait_elapsed -gt $timeout ]]; then
            error_log "[throughput-cedana] Timeout waiting for jobs to complete ($completed/${#job_names[@]} done)"
            break
        fi

        if [[ $completed -lt ${#job_names[@]} ]]; then
            sleep 5
        fi
    done

    local end_time
    end_time=$(date +%s)
    local total_wall_time=$((end_time - start_time))

    info_log "[throughput-cedana] Complete: $completed/${#job_names[@]} jobs in ${total_wall_time}s"

    # Record metrics
    record_metric "cedana" "$workload" "saturation_wall_time" "$total_wall_time"
    record_metric "cedana" "$workload" "saturation_jobs" "$num_jobs"
    record_metric "cedana" "$workload" "saturation_completed" "$completed"

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
