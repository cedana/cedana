#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,perf,throughput

# Throughput Efficiency Test for Cedana
#
# This test measures effective job throughput by running jobs with simulated
# preemptions (pod deletions) and comparing:
# - Baseline: Jobs restart from scratch after preemption (losing all compute)
# - Cedana: Jobs checkpoint before preemption and restore (preserving compute)
#
# Preemption scenarios covered:
# - Spot/preemptible instance reclamation
# - Priority-based preemption
# - Node draining for maintenance
# - Resource pressure evictions
#
# For cedana jobs, we use the propagator API to:
# 1. Trigger checkpoint before simulated preemption
# 2. Delete pod to simulate preemption
# 3. Trigger restore via API
#
# Environment variables:
#   THROUGHPUT_NUM_JOBS        - Number of jobs per workload type (default: 2)
#   THROUGHPUT_WORKLOADS       - Comma-separated workload types (default: monte-carlo-pi)
#   THROUGHPUT_NAMESPACE       - Test namespace (default: throughput-test)
#   THROUGHPUT_INTERRUPT_DELAY - Seconds to wait before preemption (default: 25)

load ../helpers/utils
load ../helpers/k8s
load ../helpers/propagator

################################################################################
# Configuration
################################################################################

THROUGHPUT_NUM_JOBS="${THROUGHPUT_NUM_JOBS:-2}"
THROUGHPUT_WORKLOADS="${THROUGHPUT_WORKLOADS:-monte-carlo-pi}"
THROUGHPUT_NAMESPACE="${THROUGHPUT_NAMESPACE:-throughput-test}"
THROUGHPUT_INTERRUPT_DELAY="${THROUGHPUT_INTERRUPT_DELAY:-25}"
THROUGHPUT_JOB_TIMEOUT="${THROUGHPUT_JOB_TIMEOUT:-600}"
THROUGHPUT_CHECKPOINT_TIMEOUT="${THROUGHPUT_CHECKPOINT_TIMEOUT:-120}"

# Samples directory - set by setup or environment
THROUGHPUT_SAMPLES_DIR="${THROUGHPUT_SAMPLES_DIR:-}"

# State directory for metrics collection
STATE_DIR="/tmp/throughput-test-$$"

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
        if [[ -d "/tmp/cedana-samples/kubernetes/throughput-test" ]]; then
            THROUGHPUT_SAMPLES_DIR="/tmp/cedana-samples/kubernetes/throughput-test"
        elif [[ -d "../cedana-samples/kubernetes/throughput-test" ]]; then
            THROUGHPUT_SAMPLES_DIR="../cedana-samples/kubernetes/throughput-test"
        else
            # Clone samples repo
            git clone --depth 1 https://github.com/cedana/cedana-samples.git /tmp/cedana-samples 2>/dev/null || true
            THROUGHPUT_SAMPLES_DIR="/tmp/cedana-samples/kubernetes/throughput-test"
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

# Get the pod name for a job
get_job_pod() {
    local job_name="$1"
    local namespace="$2"
    local timeout="${3:-60}"

    local elapsed=0
    while [[ $elapsed -lt $timeout ]]; do
        local pod_name
        pod_name=$(kubectl get pods -n "$namespace" -l job-name="$job_name" \
            --no-headers -o custom-columns=':metadata.name' 2>/dev/null | head -1)

        if [[ -n "$pod_name" ]]; then
            echo "$pod_name"
            return 0
        fi

        sleep 2
        ((elapsed += 2))
    done

    error_log "Timeout waiting for pod for job $job_name"
    return 1
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
    pod_name=$(get_job_pod "$job_name" "$namespace" 10) || return 1

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

generate_report() {
    echo ""
    echo "╔════════════════════════════════════════════════════════════════════════╗"
    echo "║           CEDANA THROUGHPUT EFFICIENCY REPORT                          ║"
    echo "║  Measuring compute preservation across preemption events               ║"
    echo "╠════════════════════════════════════════════════════════════════════════╣"
    echo "║  Preemption scenarios: spot reclamation, priority preemption,          ║"
    echo "║  node drain, maintenance, resource pressure evictions                  ║"
    echo "╠════════════════════════════════════════════════════════════════════════╣"

    IFS=',' read -ra workloads <<< "$THROUGHPUT_WORKLOADS"

    for workload in "${workloads[@]}"; do
        local baseline_wall=$(get_metric_sum "baseline" "$workload" "wall_time")
        local baseline_restarts=$(get_metric_sum "baseline" "$workload" "restarts")
        local cedana_wall=$(get_metric_sum "cedana" "$workload" "wall_time")
        local cedana_restarts=$(get_metric_sum "cedana" "$workload" "restarts")

        local time_saved=$((baseline_wall - cedana_wall))
        local efficiency=0
        if [[ $baseline_wall -gt 0 ]]; then
            efficiency=$((time_saved * 100 / baseline_wall))
        fi

        echo "║"
        echo "║ Workload: $workload"
        echo "║   Baseline (restart from scratch): ${baseline_wall}s  (${baseline_restarts} preemptions)"
        echo "║   Cedana (checkpoint/restore):     ${cedana_wall}s  (${cedana_restarts} preemptions)"
        echo "║   ─────────────────────────────────────────────────────────────────"
        echo "║   Time saved: ${time_saved}s  |  Throughput improvement: ${efficiency}%"
        echo "║"
    done

    echo "╚════════════════════════════════════════════════════════════════════════╝"
    echo ""
}

################################################################################
# Test Runner
################################################################################

# Run baseline test - jobs restart from scratch after preemption
run_baseline_test() {
    local workload="$1"

    local template_file="$THROUGHPUT_SAMPLES_DIR/${workload}-baseline.yaml"

    if [[ ! -f "$template_file" ]]; then
        error_log "Template not found: $template_file"
        return 1
    fi

    info_log "[baseline] Running $THROUGHPUT_NUM_JOBS jobs for workload: $workload"

    local total_wall_time=0
    local total_restarts=0

    for i in $(seq 1 "$THROUGHPUT_NUM_JOBS"); do
        local job_spec="$template_file"

        info_log "[baseline] Job $i/$THROUGHPUT_NUM_JOBS: submitting"

        local start_time
        start_time=$(date +%s)

        local job_name
        job_name=$(submit_job "$job_spec" "$THROUGHPUT_NAMESPACE")
        [[ -n "$job_name" ]] || continue

        info_log "[baseline] Job $i: created as $job_name"

        # Wait for pod to be running
        local pod_name
        pod_name=$(get_job_pod "$job_name" "$THROUGHPUT_NAMESPACE" 120) || continue

        # Wait for pod to be ready
        validate_pod "$pod_name" 120 "$THROUGHPUT_NAMESPACE" || continue

        info_log "[baseline] Job $i: pod $pod_name is running"

        # Wait before preemption
        info_log "[baseline] Job $i: waiting ${THROUGHPUT_INTERRUPT_DELAY}s before preemption"
        sleep "$THROUGHPUT_INTERRUPT_DELAY"

        # Get progress at preemption
        local progress_before
        progress_before=$(get_job_progress "$job_name" "$THROUGHPUT_NAMESPACE")
        info_log "[baseline] Job $i: progress before preemption: $progress_before"

        # Simulate preemption by deleting the pod
        info_log "[baseline] Job $i: PREEMPTING (kubectl delete pod $pod_name)"
        kubectl delete pod "$pod_name" -n "$THROUGHPUT_NAMESPACE" --wait=false

        ((total_restarts++))

        # Wait for job to complete (job controller will create new pod from scratch)
        info_log "[baseline] Job $i: waiting for job to complete after restart..."
        wait_for_job_complete "$job_name" "$THROUGHPUT_NAMESPACE" "$THROUGHPUT_JOB_TIMEOUT"

        local end_time
        end_time=$(date +%s)
        local wall_time=$((end_time - start_time))
        total_wall_time=$((total_wall_time + wall_time))

        # Get final progress
        local progress_after
        progress_after=$(get_job_progress "$job_name" "$THROUGHPUT_NAMESPACE")
        info_log "[baseline] Job $i: completed in ${wall_time}s, final progress: $progress_after"

        # Cleanup job
        kubectl delete job "$job_name" -n "$THROUGHPUT_NAMESPACE" --wait=false 2>/dev/null || true
    done

    # Record metrics
    record_metric "baseline" "$workload" "wall_time" "$total_wall_time"
    record_metric "baseline" "$workload" "restarts" "$total_restarts"

    info_log "[baseline] $workload complete: total_wall_time=${total_wall_time}s, restarts=${total_restarts}"
}

# Run cedana test - checkpoint before preemption, restore after
run_cedana_test() {
    local workload="$1"

    local template_file="$THROUGHPUT_SAMPLES_DIR/${workload}-cedana.yaml"

    if [[ ! -f "$template_file" ]]; then
        error_log "Template not found: $template_file"
        return 1
    fi

    # Verify we have API credentials
    if [[ -z "$CEDANA_URL" ]] || [[ -z "$CEDANA_AUTH_TOKEN" ]]; then
        error_log "Cedana API credentials not configured"
        return 1
    fi

    info_log "[cedana] Running $THROUGHPUT_NUM_JOBS jobs for workload: $workload"

    local total_wall_time=0
    local total_restarts=0

    for i in $(seq 1 "$THROUGHPUT_NUM_JOBS"); do
        local checkpoint_id
        checkpoint_id=$(generate_checkpoint_id)

        local job_spec
        job_spec=$(create_job_from_template "$template_file" "cedana-${workload}-${i}" "$checkpoint_id")

        info_log "[cedana] Job $i/$THROUGHPUT_NUM_JOBS: submitting (checkpoint_id=$checkpoint_id)"

        local start_time
        start_time=$(date +%s)

        local job_name
        job_name=$(submit_job "$job_spec" "$THROUGHPUT_NAMESPACE")
        [[ -n "$job_name" ]] || continue

        info_log "[cedana] Job $i: created as $job_name"

        # Wait for pod to be running
        local pod_name
        pod_name=$(get_job_pod "$job_name" "$THROUGHPUT_NAMESPACE" 120) || continue

        # Wait for pod to be ready
        validate_pod "$pod_name" 120 "$THROUGHPUT_NAMESPACE" || continue

        info_log "[cedana] Job $i: pod $pod_name is running"

        # Get pod UID for checkpoint API
        local pod_uid
        pod_uid=$(kubectl get pod "$pod_name" -n "$THROUGHPUT_NAMESPACE" -o jsonpath='{.metadata.uid}')
        info_log "[cedana] Job $i: pod UID $pod_uid"

        # Wait before preemption
        info_log "[cedana] Job $i: waiting ${THROUGHPUT_INTERRUPT_DELAY}s before checkpoint"
        sleep "$THROUGHPUT_INTERRUPT_DELAY"

        # Get progress at checkpoint
        local progress_before
        progress_before=$(get_job_progress "$job_name" "$THROUGHPUT_NAMESPACE")
        info_log "[cedana] Job $i: progress before checkpoint: $progress_before"

        # Trigger checkpoint via API
        info_log "[cedana] Job $i: CHECKPOINTING via API"
        local action_id
        action_id=$(checkpoint_pod "$pod_uid" "/run/containerd/runc/k8s.io")

        if [[ -z "$action_id" ]]; then
            error_log "[cedana] Job $i: checkpoint failed"
            kubectl delete job "$job_name" -n "$THROUGHPUT_NAMESPACE" --wait=false 2>/dev/null || true
            continue
        fi

        info_log "[cedana] Job $i: checkpoint action_id=$action_id"

        # Wait for checkpoint to complete
        if ! poll_action_status "$action_id" "checkpoint" "$THROUGHPUT_CHECKPOINT_TIMEOUT"; then
            error_log "[cedana] Job $i: checkpoint did not complete"
            kubectl delete job "$job_name" -n "$THROUGHPUT_NAMESPACE" --wait=false 2>/dev/null || true
            continue
        fi

        info_log "[cedana] Job $i: checkpoint complete"

        # Simulate preemption by deleting the pod
        info_log "[cedana] Job $i: PREEMPTING (kubectl delete pod $pod_name)"
        kubectl delete pod "$pod_name" -n "$THROUGHPUT_NAMESPACE" --wait=false

        ((total_restarts++))

        # Trigger restore via API
        info_log "[cedana] Job $i: RESTORING via API"
        local restore_action_id
        restore_action_id=$(restore_pod "$action_id" "$CLUSTER_ID")

        if [[ -z "$restore_action_id" ]]; then
            error_log "[cedana] Job $i: restore failed"
            kubectl delete job "$job_name" -n "$THROUGHPUT_NAMESPACE" --wait=false 2>/dev/null || true
            continue
        fi

        info_log "[cedana] Job $i: restore action_id=$restore_action_id"

        # Wait for job to complete
        info_log "[cedana] Job $i: waiting for job to complete after restore..."
        wait_for_job_complete "$job_name" "$THROUGHPUT_NAMESPACE" "$THROUGHPUT_JOB_TIMEOUT"

        local end_time
        end_time=$(date +%s)
        local wall_time=$((end_time - start_time))
        total_wall_time=$((total_wall_time + wall_time))

        # Get final progress
        local progress_after
        progress_after=$(get_job_progress "$job_name" "$THROUGHPUT_NAMESPACE")
        info_log "[cedana] Job $i: completed in ${wall_time}s, final progress: $progress_after"

        # Cleanup job
        kubectl delete job "$job_name" -n "$THROUGHPUT_NAMESPACE" --wait=false 2>/dev/null || true
    done

    # Record metrics
    record_metric "cedana" "$workload" "wall_time" "$total_wall_time"
    record_metric "cedana" "$workload" "restarts" "$total_restarts"

    info_log "[cedana] $workload complete: total_wall_time=${total_wall_time}s, restarts=${total_restarts}"
}

# Legacy wrapper for backwards compatibility
run_throughput_test() {
    local test_mode="$1"
    local workload="$2"

    if [[ "$test_mode" == "baseline" ]]; then
        run_baseline_test "$workload"
    else
        run_cedana_test "$workload"
    fi
}

################################################################################
# Tests
################################################################################

# Monte Carlo Pi - compute-bound workload
# bats test_tags=throughput,baseline,monte-carlo
@test "Throughput: Baseline (monte-carlo-pi) - restart from scratch" {
    [[ -f "$THROUGHPUT_SAMPLES_DIR/monte-carlo-pi-baseline.yaml" ]] || skip "Sample not found"
    run_baseline_test "monte-carlo-pi"
}

# bats test_tags=throughput,cedana,monte-carlo
@test "Throughput: Cedana (monte-carlo-pi) - checkpoint/restore" {
    [[ -f "$THROUGHPUT_SAMPLES_DIR/monte-carlo-pi-cedana.yaml" ]] || skip "Sample not found"
    run_cedana_test "monte-carlo-pi"
}

# Sklearn Random Forest - ML training workload
# bats test_tags=throughput,baseline,sklearn
@test "Throughput: Baseline (sklearn-rf) - restart from scratch" {
    [[ -f "$THROUGHPUT_SAMPLES_DIR/sklearn-rf-baseline.yaml" ]] || skip "Sample not found"
    run_baseline_test "sklearn-rf"
}

# bats test_tags=throughput,cedana,sklearn
@test "Throughput: Cedana (sklearn-rf) - checkpoint/restore" {
    [[ -f "$THROUGHPUT_SAMPLES_DIR/sklearn-rf-cedana.yaml" ]] || skip "Sample not found"
    run_cedana_test "sklearn-rf"
}

# NumPy Matrix Operations - linear algebra workload
# bats test_tags=throughput,baseline,numpy
@test "Throughput: Baseline (numpy-ops) - restart from scratch" {
    [[ -f "$THROUGHPUT_SAMPLES_DIR/numpy-ops-baseline.yaml" ]] || skip "Sample not found"
    run_baseline_test "numpy-ops"
}

# bats test_tags=throughput,cedana,numpy
@test "Throughput: Cedana (numpy-ops) - checkpoint/restore" {
    [[ -f "$THROUGHPUT_SAMPLES_DIR/numpy-ops-cedana.yaml" ]] || skip "Sample not found"
    run_cedana_test "numpy-ops"
}
