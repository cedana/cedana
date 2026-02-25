#!/usr/bin/env bash
#
# Cedana Kubernetes Throughput Efficiency Test
#
# This test measures effective job throughput by comparing:
# 1. Baseline: Jobs run to completion with random interruptions (kubectl delete)
#              Interrupted jobs must restart from scratch, wasting all prior compute
# 2. Cedana: Jobs run with runtimeClassName=cedana. When interrupted, they restore
#            from checkpoint, preserving prior compute work
#
# The key metric is "Effective Throughput":
#   - For baseline: Total wall-clock time to complete all jobs (including restarts)
#   - For cedana: Total wall-clock time to complete all jobs (with checkpoint/restore)
#
# Efficiency = (Baseline Time - Cedana Time) / Baseline Time
#
# Usage:
#   ./perf-k8s.sh [options]
#
# Options:
#   -n, --num-jobs N       Number of jobs to run (default: 5)
#   -N, --num-nodes N      Number of nodes to distribute jobs across (default: 5)
#   -i, --interruptions N  Number of interruptions to inject per test run (default: 2)
#   -w, --workload TYPE    Workload type: cpu, gpu, training (default: cpu)
#   -d, --duration SECS    Target job duration in seconds (default: 300)
#   -c, --cedana-only      Only run the cedana test (skip baseline)
#   -b, --baseline-only    Only run the baseline test (skip cedana)
#   -o, --output FILE      Output results to JSON file
#   -v, --verbose          Enable verbose logging
#   -h, --help             Show this help message

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load helpers
source "$SCRIPT_DIR/helpers/utils.bash"
source "$SCRIPT_DIR/helpers/k8s.bash"
source "$SCRIPT_DIR/helpers/propagator.bash"

################################################################################
# Configuration
################################################################################

NUM_JOBS="${NUM_JOBS:-5}"
NUM_NODES="${NUM_NODES:-5}"
NUM_INTERRUPTIONS="${NUM_INTERRUPTIONS:-2}"
WORKLOAD_TYPE="${WORKLOAD_TYPE:-cpu}"
TARGET_DURATION="${TARGET_DURATION:-300}"
OUTPUT_FILE="${OUTPUT_FILE:-}"
VERBOSE="${VERBOSE:-false}"
RUN_BASELINE="${RUN_BASELINE:-true}"
RUN_CEDANA="${RUN_CEDANA:-true}"

PERF_NAMESPACE="${PERF_NAMESPACE:-perf-test}"
STATE_DIR="/tmp/perf-test-state-$$"

# Workload configurations
declare -A WORKLOAD_IMAGES=(
    ["cpu"]="python:3.11-slim"
    ["gpu"]="nvcr.io/nvidia/cuda:12.4.0-devel-ubuntu22.04"
    ["training"]="pytorch/pytorch:2.2.0-cuda12.1-cudnn8-runtime"
)

# CPU workload: Pi calculation that scales with iterations
CPU_WORKLOAD_SCRIPT='
import time
import sys
import signal

checkpoint_file = "/tmp/progress.txt"
done = False

def signal_handler(sig, frame):
    global done
    done = True

signal.signal(signal.SIGTERM, signal_handler)

def load_progress():
    try:
        with open(checkpoint_file, "r") as f:
            return int(f.read().strip())
    except:
        return 0

def save_progress(iteration):
    with open(checkpoint_file, "w") as f:
        f.write(str(iteration))

def compute_pi_chunk(start, end):
    """Leibniz formula for Pi, computed in chunks"""
    pi = 0.0
    for k in range(start, end):
        pi += ((-1)**k) / (2*k + 1)
    return 4 * pi

target_iterations = int(sys.argv[1]) if len(sys.argv) > 1 else 1000000
chunk_size = 10000
start_iteration = load_progress()

print(f"Starting from iteration {start_iteration}, target: {target_iterations}")
sys.stdout.flush()

iteration = start_iteration
while iteration < target_iterations and not done:
    end = min(iteration + chunk_size, target_iterations)
    pi = compute_pi_chunk(iteration, end)
    iteration = end
    save_progress(iteration)
    progress = (iteration / target_iterations) * 100
    print(f"Progress: {progress:.1f}% (iteration {iteration}/{target_iterations}, pi ≈ {pi:.10f})")
    sys.stdout.flush()
    time.sleep(0.1)

if done:
    print(f"Graceful shutdown at iteration {iteration}")
else:
    print(f"COMPLETED: Final iteration {iteration}")
sys.stdout.flush()
'

################################################################################
# Parse Arguments
################################################################################

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -n|--num-jobs)
                NUM_JOBS="$2"
                shift 2
                ;;
            -N|--num-nodes)
                NUM_NODES="$2"
                shift 2
                ;;
            -i|--interruptions)
                NUM_INTERRUPTIONS="$2"
                shift 2
                ;;
            -w|--workload)
                WORKLOAD_TYPE="$2"
                shift 2
                ;;
            -d|--duration)
                TARGET_DURATION="$2"
                shift 2
                ;;
            -c|--cedana-only)
                RUN_BASELINE=false
                shift
                ;;
            -b|--baseline-only)
                RUN_CEDANA=false
                shift
                ;;
            -o|--output)
                OUTPUT_FILE="$2"
                shift 2
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                error_log "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
}

show_help() {
    head -50 "$0" | grep "^#" | sed 's/^# //' | sed 's/^#//'
}

################################################################################
# State Management
################################################################################

init_state() {
    rm -rf "$STATE_DIR"
    mkdir -p "$STATE_DIR"
    mkdir -p "$STATE_DIR/jobs"
    mkdir -p "$STATE_DIR/metrics"
}

cleanup_state() {
    rm -rf "$STATE_DIR"
}

# Job state tracking
set_job_state() {
    local job_name="$1" key="$2" value="$3"
    echo "$value" > "$STATE_DIR/jobs/${job_name}_${key}"
}

get_job_state() {
    local job_name="$1" key="$2"
    cat "$STATE_DIR/jobs/${job_name}_${key}" 2>/dev/null || echo ""
}

# Metrics recording
record_metric() {
    local test_type="$1" metric="$2" value="$3"
    echo "$value" >> "$STATE_DIR/metrics/${test_type}_${metric}"
}

get_metric_sum() {
    local test_type="$1" metric="$2"
    local file="$STATE_DIR/metrics/${test_type}_${metric}"
    if [[ -f "$file" ]]; then
        awk '{sum+=$1} END {print sum}' "$file"
    else
        echo "0"
    fi
}

get_metric_count() {
    local test_type="$1" metric="$2"
    local file="$STATE_DIR/metrics/${test_type}_${metric}"
    if [[ -f "$file" ]]; then
        wc -l < "$file"
    else
        echo "0"
    fi
}

################################################################################
# Job Spec Generation
################################################################################

# Generate a CPU workload pod spec
generate_cpu_job_spec() {
    local name="$1"
    local namespace="$2"
    local use_cedana="$3"
    local iterations="${4:-1000000}"

    local spec="/tmp/perf-job-${name}.yaml"

    cat > "$spec" << EOF
apiVersion: v1
kind: Pod
metadata:
  name: ${name}
  namespace: ${namespace}
  labels:
    app: perf-test
    perf-test/job: ${name}
    perf-test/type: cpu
spec:
EOF

    if [[ "$use_cedana" == "true" ]]; then
        cat >> "$spec" << EOF
  runtimeClassName: cedana
EOF
    fi

    cat >> "$spec" << EOF
  restartPolicy: Never
  containers:
  - name: workload
    image: ${WORKLOAD_IMAGES[cpu]}
    command: ["/bin/sh", "-c"]
    args:
      - |
        cat > /tmp/workload.py << 'SCRIPT'
${CPU_WORKLOAD_SCRIPT}
        SCRIPT
        python /tmp/workload.py ${iterations}
    resources:
      requests:
        cpu: "500m"
        memory: "256Mi"
      limits:
        cpu: "1"
        memory: "512Mi"
EOF

    echo "$spec"
}

# Generate a GPU workload pod spec (placeholder for now)
generate_gpu_job_spec() {
    local name="$1"
    local namespace="$2"
    local use_cedana="$3"

    # For now, use the samples directory
    local spec="/tmp/perf-job-${name}.yaml"

    # TODO: Implement GPU workload
    generate_cpu_job_spec "$name" "$namespace" "$use_cedana"
}

################################################################################
# Job Management
################################################################################

deploy_job() {
    local name="$1"
    local namespace="$2"
    local use_cedana="$3"

    local spec
    case "$WORKLOAD_TYPE" in
        cpu)
            spec=$(generate_cpu_job_spec "$name" "$namespace" "$use_cedana" 500000)
            ;;
        gpu)
            spec=$(generate_gpu_job_spec "$name" "$namespace" "$use_cedana")
            ;;
        *)
            spec=$(generate_cpu_job_spec "$name" "$namespace" "$use_cedana" 500000)
            ;;
    esac

    local start_time
    start_time=$(date +%s%N)

    kubectl apply -f "$spec"
    set_job_state "$name" "start_time" "$start_time"
    set_job_state "$name" "status" "running"
    set_job_state "$name" "restarts" "0"
    set_job_state "$name" "use_cedana" "$use_cedana"

    info_log "Deployed job: $name (cedana=$use_cedana)"
}

wait_for_job_ready() {
    local name="$1"
    local namespace="$2"
    local timeout="${3:-120}"

    if ! validate_pod "$name" "$timeout" "$namespace"; then
        error_log "Job $name failed to become ready"
        return 1
    fi

    info_log "Job $name is ready"
    return 0
}

interrupt_job() {
    local name="$1"
    local namespace="$2"
    local use_cedana="$3"

    info_log "INTERRUPTING job: $name"

    local interrupt_time
    interrupt_time=$(date +%s%N)
    set_job_state "$name" "interrupt_time" "$interrupt_time"

    if [[ "$use_cedana" == "true" ]]; then
        # For cedana: checkpoint first, then delete
        local pod_id
        pod_id=$(get_pod_id "$name" "$namespace" 2>/dev/null) || true

        if [[ -n "$pod_id" ]]; then
            local action_id
            action_id=$(checkpoint_pod "$pod_id" 2>/dev/null) || true

            if [[ -n "$action_id" ]] && validate_action_id "$action_id" 2>/dev/null; then
                set_job_state "$name" "action_id" "$action_id"
                # Wait for checkpoint to complete before deleting
                poll_action_status "$action_id" "checkpoint" 120 || true
            fi
        fi
    fi

    # Delete the pod (simulating interruption)
    kubectl delete pod "$name" -n "$namespace" --wait=false --timeout=30s 2>/dev/null || true

    local restarts
    restarts=$(get_job_state "$name" "restarts")
    restarts=$((restarts + 1))
    set_job_state "$name" "restarts" "$restarts"
    set_job_state "$name" "status" "interrupted"
}

restart_or_restore_job() {
    local name="$1"
    local namespace="$2"
    local use_cedana="$3"

    # Wait for pod to be fully deleted
    local wait_count=0
    while kubectl get pod "$name" -n "$namespace" &>/dev/null && [[ $wait_count -lt 30 ]]; do
        sleep 1
        ((wait_count++))
    done

    if [[ "$use_cedana" == "true" ]]; then
        # Restore from checkpoint
        local action_id
        action_id=$(get_job_state "$name" "action_id")

        if [[ -n "$action_id" ]]; then
            info_log "RESTORING job: $name from checkpoint"
            local restore_id
            restore_id=$(restore_pod "$action_id" "$CLUSTER_ID" 2>/dev/null) || true

            if [[ -n "$restore_id" ]]; then
                # Wait for restored pod to appear
                local restored_name
                restored_name=$(wait_for_cmd 60 get_restored_pod "$name" "$namespace") || true

                if [[ -n "$restored_name" ]]; then
                    set_job_state "$name" "restored_name" "$restored_name"
                    set_job_state "$name" "status" "running"
                    info_log "Job $name restored as $restored_name"
                    return 0
                fi
            fi
        fi

        # Fallback: restart from scratch if restore fails
        warn_log "Restore failed for $name, restarting from scratch"
    fi

    # Restart from scratch (baseline behavior)
    info_log "RESTARTING job: $name from scratch"
    deploy_job "$name" "$namespace" "$use_cedana"
}

is_job_complete() {
    local name="$1"
    local namespace="$2"

    # Check if original pod completed
    local phase
    phase=$(kubectl get pod "$name" -n "$namespace" -o jsonpath='{.status.phase}' 2>/dev/null) || phase=""

    if [[ "$phase" == "Succeeded" ]]; then
        return 0
    fi

    # Check restored pod if applicable
    local restored_name
    restored_name=$(get_job_state "$name" "restored_name")
    if [[ -n "$restored_name" ]]; then
        phase=$(kubectl get pod "$restored_name" -n "$namespace" -o jsonpath='{.status.phase}' 2>/dev/null) || phase=""
        if [[ "$phase" == "Succeeded" ]]; then
            return 0
        fi
    fi

    # Check logs for completion marker
    local active_pod="$name"
    [[ -n "$restored_name" ]] && active_pod="$restored_name"

    if kubectl logs "$active_pod" -n "$namespace" 2>/dev/null | grep -q "COMPLETED:"; then
        return 0
    fi

    return 1
}

wait_for_all_jobs_complete() {
    local namespace="$1"
    local timeout="${2:-1800}"
    local job_names=("${@:3}")

    local start_time end_time
    start_time=$(date +%s)
    end_time=$((start_time + timeout))

    local completed=0
    local total=${#job_names[@]}

    while [[ $(date +%s) -lt $end_time ]]; do
        completed=0

        for name in "${job_names[@]}"; do
            if is_job_complete "$name" "$namespace"; then
                local status
                status=$(get_job_state "$name" "status")
                if [[ "$status" != "completed" ]]; then
                    local end_ns
                    end_ns=$(date +%s%N)
                    set_job_state "$name" "end_time" "$end_ns"
                    set_job_state "$name" "status" "completed"
                fi
                ((completed++))
            fi
        done

        info_log "Progress: $completed/$total jobs completed"

        if [[ $completed -eq $total ]]; then
            return 0
        fi

        sleep 5
    done

    error_log "Timeout waiting for jobs to complete (completed: $completed/$total)"
    return 1
}

################################################################################
# Test Execution
################################################################################

run_test() {
    local test_type="$1"  # "baseline" or "cedana"
    local use_cedana="false"
    [[ "$test_type" == "cedana" ]] && use_cedana="true"

    info_log "=============================================="
    info_log "Starting $test_type test"
    info_log "  Jobs: $NUM_JOBS"
    info_log "  Interruptions: $NUM_INTERRUPTIONS"
    info_log "  Workload: $WORKLOAD_TYPE"
    info_log "  Cedana: $use_cedana"
    info_log "=============================================="

    local namespace="${PERF_NAMESPACE}-${test_type}"
    create_namespace "$namespace"

    # Deploy all jobs
    local job_names=()
    for i in $(seq 1 "$NUM_JOBS"); do
        local name="perf-job-${test_type}-${i}-$(date +%s)"
        job_names+=("$name")
        deploy_job "$name" "$namespace" "$use_cedana"
    done

    # Wait for all jobs to be ready
    for name in "${job_names[@]}"; do
        wait_for_job_ready "$name" "$namespace" 180 || true
    done

    # Schedule random interruptions
    local interruption_times=()
    local total_expected_time=$((TARGET_DURATION / 2))  # Interrupt in first half
    for i in $(seq 1 "$NUM_INTERRUPTIONS"); do
        local delay=$((RANDOM % total_expected_time + 10))
        interruption_times+=("$delay")
    done

    # Sort interruption times
    IFS=$'\n' interruption_times=($(sort -n <<<"${interruption_times[*]}")); unset IFS

    info_log "Scheduled interruptions at: ${interruption_times[*]} seconds"

    # Run the test with interruptions
    local test_start_time
    test_start_time=$(date +%s)
    local last_interrupt_time=0

    for delay in "${interruption_times[@]}"; do
        # Wait until next interruption
        local wait_time=$((delay - last_interrupt_time))
        [[ $wait_time -gt 0 ]] && sleep "$wait_time"
        last_interrupt_time=$delay

        # Pick a random running job to interrupt
        local running_jobs=()
        for name in "${job_names[@]}"; do
            local status
            status=$(get_job_state "$name" "status")
            if [[ "$status" == "running" ]] && ! is_job_complete "$name" "$namespace"; then
                running_jobs+=("$name")
            fi
        done

        if [[ ${#running_jobs[@]} -eq 0 ]]; then
            info_log "No running jobs to interrupt"
            continue
        fi

        local target_job="${running_jobs[$((RANDOM % ${#running_jobs[@]}))]}"

        # Record compute time lost/preserved
        local job_start
        job_start=$(get_job_state "$target_job" "start_time")
        local current_time_ns
        current_time_ns=$(date +%s%N)
        local elapsed_ns=$((current_time_ns - job_start))
        local elapsed_sec=$((elapsed_ns / 1000000000))

        record_metric "$test_type" "interrupted_compute_seconds" "$elapsed_sec"

        # Interrupt and restart/restore
        interrupt_job "$target_job" "$namespace" "$use_cedana"
        sleep 2
        restart_or_restore_job "$target_job" "$namespace" "$use_cedana"

        # Wait for job to be ready again
        local active_name="$target_job"
        local restored_name
        restored_name=$(get_job_state "$target_job" "restored_name")
        [[ -n "$restored_name" ]] && active_name="$restored_name"

        wait_for_job_ready "$active_name" "$namespace" 180 || true
    done

    # Wait for all jobs to complete
    wait_for_all_jobs_complete "$namespace" 1800 "${job_names[@]}"

    local test_end_time
    test_end_time=$(date +%s)
    local total_wall_time=$((test_end_time - test_start_time))

    # Calculate metrics
    local total_restarts=0
    local total_job_time=0

    for name in "${job_names[@]}"; do
        local restarts
        restarts=$(get_job_state "$name" "restarts")
        total_restarts=$((total_restarts + restarts))

        local start_time end_time job_duration
        start_time=$(get_job_state "$name" "start_time")
        end_time=$(get_job_state "$name" "end_time")
        if [[ -n "$start_time" ]] && [[ -n "$end_time" ]]; then
            job_duration=$(( (end_time - start_time) / 1000000000 ))
            total_job_time=$((total_job_time + job_duration))
        fi
    done

    local interrupted_compute
    interrupted_compute=$(get_metric_sum "$test_type" "interrupted_compute_seconds")

    # Record final metrics
    record_metric "$test_type" "total_wall_time_seconds" "$total_wall_time"
    record_metric "$test_type" "total_restarts" "$total_restarts"
    record_metric "$test_type" "total_interrupted_compute_seconds" "$interrupted_compute"

    info_log "=============================================="
    info_log "$test_type Test Results"
    info_log "  Total wall time: ${total_wall_time}s"
    info_log "  Total restarts: $total_restarts"
    info_log "  Interrupted compute: ${interrupted_compute}s"
    info_log "=============================================="

    # Cleanup
    delete_namespace "$namespace" --force --timeout=120s 2>/dev/null || true

    echo "$total_wall_time"
}

################################################################################
# Results and Reporting
################################################################################

generate_report() {
    local baseline_time="$1"
    local cedana_time="$2"

    local baseline_interrupted cedana_interrupted
    baseline_interrupted=$(get_metric_sum "baseline" "interrupted_compute_seconds")
    cedana_interrupted=$(get_metric_sum "cedana" "interrupted_compute_seconds")

    local baseline_restarts cedana_restarts
    baseline_restarts=$(get_metric_sum "baseline" "total_restarts")
    cedana_restarts=$(get_metric_sum "cedana" "total_restarts")

    local time_saved efficiency
    time_saved=$((baseline_time - cedana_time))
    if [[ $baseline_time -gt 0 ]]; then
        efficiency=$(awk "BEGIN {printf \"%.2f\", ($time_saved / $baseline_time) * 100}")
    else
        efficiency="0"
    fi

    local compute_preserved
    if [[ $baseline_interrupted -gt 0 ]]; then
        # For cedana, the interrupted compute is mostly preserved via restore
        compute_preserved=$(awk "BEGIN {printf \"%.2f\", ($cedana_interrupted / $baseline_interrupted) * 100}")
    else
        compute_preserved="0"
    fi

    echo ""
    echo "╔════════════════════════════════════════════════════════════════╗"
    echo "║            CEDANA THROUGHPUT EFFICIENCY REPORT                 ║"
    echo "╠════════════════════════════════════════════════════════════════╣"
    echo "║ Configuration                                                  ║"
    echo "║   Jobs: $NUM_JOBS                                                        ║"
    echo "║   Interruptions per test: $NUM_INTERRUPTIONS                               ║"
    echo "║   Workload type: $WORKLOAD_TYPE                                         ║"
    echo "╠════════════════════════════════════════════════════════════════╣"
    echo "║ Results                                                        ║"
    echo "║                          Baseline        Cedana                ║"
    echo "║   Total wall time:      ${baseline_time}s              ${cedana_time}s                 ║"
    echo "║   Total restarts:       ${baseline_restarts}                 ${cedana_restarts}                  ║"
    echo "║   Interrupted compute:  ${baseline_interrupted}s              ${cedana_interrupted}s                 ║"
    echo "╠════════════════════════════════════════════════════════════════╣"
    echo "║ Efficiency Metrics                                             ║"
    echo "║   Time saved: ${time_saved}s                                            ║"
    echo "║   Throughput efficiency gain: ${efficiency}%                          ║"
    echo "║   Compute preserved: ${compute_preserved}%                                ║"
    echo "╚════════════════════════════════════════════════════════════════╝"
    echo ""

    if [[ -n "$OUTPUT_FILE" ]]; then
        cat > "$OUTPUT_FILE" << EOF
{
  "configuration": {
    "num_jobs": $NUM_JOBS,
    "num_interruptions": $NUM_INTERRUPTIONS,
    "workload_type": "$WORKLOAD_TYPE",
    "target_duration_seconds": $TARGET_DURATION
  },
  "baseline": {
    "total_wall_time_seconds": $baseline_time,
    "total_restarts": $baseline_restarts,
    "interrupted_compute_seconds": $baseline_interrupted
  },
  "cedana": {
    "total_wall_time_seconds": $cedana_time,
    "total_restarts": $cedana_restarts,
    "interrupted_compute_seconds": $cedana_interrupted
  },
  "efficiency": {
    "time_saved_seconds": $time_saved,
    "throughput_efficiency_percent": $efficiency,
    "compute_preserved_percent": $compute_preserved
  },
  "timestamp": "$(date -Iseconds)"
}
EOF
        info_log "Results written to: $OUTPUT_FILE"
    fi
}

################################################################################
# Main
################################################################################

main() {
    parse_args "$@"

    # Validate environment
    if [[ -z "${CEDANA_URL:-}" ]]; then
        error_log "CEDANA_URL is required"
        exit 1
    fi
    if [[ -z "${CEDANA_AUTH_TOKEN:-}" ]]; then
        error_log "CEDANA_AUTH_TOKEN is required"
        exit 1
    fi
    if [[ -z "${CLUSTER_ID:-}" ]]; then
        error_log "CLUSTER_ID is required"
        exit 1
    fi

    # Validate connectivity
    if ! kubectl cluster-info &>/dev/null; then
        error_log "Cannot connect to Kubernetes cluster"
        exit 1
    fi

    validate_propagator_connectivity || {
        error_log "Cannot connect to Cedana propagator"
        exit 1
    }

    init_state
    trap cleanup_state EXIT

    local baseline_time=0
    local cedana_time=0

    if [[ "$RUN_BASELINE" == "true" ]]; then
        baseline_time=$(run_test "baseline")
    fi

    if [[ "$RUN_CEDANA" == "true" ]]; then
        cedana_time=$(run_test "cedana")
    fi

    if [[ "$RUN_BASELINE" == "true" ]] && [[ "$RUN_CEDANA" == "true" ]]; then
        generate_report "$baseline_time" "$cedana_time"
    else
        if [[ "$RUN_BASELINE" == "true" ]]; then
            info_log "Baseline test completed in ${baseline_time}s"
        fi
        if [[ "$RUN_CEDANA" == "true" ]]; then
            info_log "Cedana test completed in ${cedana_time}s"
        fi
    fi
}

main "$@"
