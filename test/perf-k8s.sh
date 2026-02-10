#!/bin/bash
set -e

# Performance comparison script for Kubernetes workloads
# Measures pod completion time with and without runtimeClassName: cedana
# Now measures actual container runtime (excluding scheduling/image pull)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load helpers
source "$SCRIPT_DIR/helpers/utils.bash"
source "$SCRIPT_DIR/helpers/k8s.bash"
source "$SCRIPT_DIR/helpers/perf.bash"

#######################################
# Configuration
#######################################

show_help() {
    cat <<EOF
Usage: $0 [OPTIONS]

Measure Kubernetes pod completion time with and without Cedana runtime.
Times are measured as actual container runtime (excluding scheduling and image pull).

OPTIONS:
    -w, --workload FILE     Path to workload YAML file
                            Default: cedana-samples pytorch cifar100
    -n, --samples N         Number of samples per variant (default: 3)
    -e, --epochs N          Override epochs for training workloads
    -t, --timeout N         Timeout per pod in seconds (default: 1800)
    --namespace NS          Kubernetes namespace (default: perf-test)
    --baseline-only         Only run baseline (no cedana runtime)
    --cedana-only           Only run with cedana runtime
    --json                  Output results as JSON
    --json-file FILE        Write JSON results to file
    --no-interleave         Run all baseline first, then all cedana
                            (default: interleaved for fairness)
    --profile               Enable profiling (sets ENABLE_PROFILING=1)
    --profile-dir DIR       Directory to save profiling data
                            (default: ./perf-results/<timestamp>/)
    -h, --help              Show this help message

EXAMPLES:
    # Run with default pytorch workload, 3 samples, 2 epochs
    $0 --epochs 2

    # Run 5 samples with custom timeout
    $0 --samples 5 --timeout 3600 --epochs 1

    # Use custom workload YAML
    $0 --workload /path/to/my-workload.yaml --samples 3

    # Output JSON results
    $0 --epochs 2 --json --json-file results.json

    # Enable profiling and save to custom directory
    $0 --epochs 2 --profile --profile-dir ./my-results/

NOTES:
    - Workloads must be Pods that exit (Succeeded phase) when complete
    - The script modifies the YAML to add/remove runtimeClassName
    - Interleaved runs (default) reduce impact of cluster state changes
    - Ensure Cedana is installed and the cedana runtime class exists
    - Timing is now based on container runtime from k8s pod status
      (excludes scheduling time, image pull, etc.)
EOF
}

# Defaults
WORKLOAD=""
SAMPLES=3
EPOCHS=""
TIMEOUT=1800
NAMESPACE="perf-test"
BASELINE_ONLY=0
CEDANA_ONLY=0
JSON_OUTPUT=0
JSON_FILE=""
INTERLEAVE=1
PROFILE_ENABLED=0
PROFILE_DIR=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -w|--workload)
            WORKLOAD="$2"
            shift 2
            ;;
        -n|--samples)
            SAMPLES="$2"
            shift 2
            ;;
        -e|--epochs)
            EPOCHS="$2"
            shift 2
            ;;
        -t|--timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        --namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        --baseline-only)
            BASELINE_ONLY=1
            shift
            ;;
        --cedana-only)
            CEDANA_ONLY=1
            shift
            ;;
        --json)
            JSON_OUTPUT=1
            shift
            ;;
        --json-file)
            JSON_FILE="$2"
            JSON_OUTPUT=1
            shift 2
            ;;
        --no-interleave)
            INTERLEAVE=0
            shift
            ;;
        --profile)
            PROFILE_ENABLED=1
            shift
            ;;
        --profile-dir)
            PROFILE_DIR="$2"
            PROFILE_ENABLED=1
            shift 2
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Set default profile directory if profiling enabled but no dir specified
if [ $PROFILE_ENABLED -eq 1 ] && [ -z "$PROFILE_DIR" ]; then
    PROFILE_DIR="./perf-results/$(date +%Y%m%d-%H%M%S)"
fi

# Find default workload if not specified
if [ -z "$WORKLOAD" ]; then
    # Look for cedana-samples
    if [ -n "$SAMPLES_DIR" ] && [ -f "$SAMPLES_DIR/gpu/cuda-pytorch-cifar100.yaml" ]; then
        WORKLOAD="$SAMPLES_DIR/gpu/cuda-pytorch-cifar100.yaml"
    elif [ -f "$SCRIPT_DIR/../cedana-samples/kubernetes/gpu/cuda-pytorch-cifar100.yaml" ]; then
        WORKLOAD="$SCRIPT_DIR/../cedana-samples/kubernetes/gpu/cuda-pytorch-cifar100.yaml"
    elif [ -f "/tmp/cedana-samples/kubernetes/gpu/cuda-pytorch-cifar100.yaml" ]; then
        WORKLOAD="/tmp/cedana-samples/kubernetes/gpu/cuda-pytorch-cifar100.yaml"
    else
        echo "ERROR: No workload specified and default pytorch workload not found"
        echo "Please specify a workload with --workload or set SAMPLES_DIR"
        exit 1
    fi
fi

if [ ! -f "$WORKLOAD" ]; then
    echo "ERROR: Workload file not found: $WORKLOAD"
    exit 1
fi

export NAMESPACE

#######################################
# Pre-flight checks
#######################################

echo "========================================"
echo "Kubernetes Performance Test"
echo "========================================"
echo ""

# Check kubectl
if ! command -v kubectl &>/dev/null; then
    echo "ERROR: kubectl not found"
    exit 1
fi

# Check cluster connectivity
if ! kubectl cluster-info &>/dev/null; then
    echo "ERROR: Cannot connect to Kubernetes cluster"
    exit 1
fi
echo "✓ Connected to cluster: $(kubectl config current-context)"

# Check for cedana runtime class (unless baseline-only)
if [ $BASELINE_ONLY -eq 0 ]; then
    if ! kubectl get runtimeclass cedana &>/dev/null; then
        echo "ERROR: RuntimeClass 'cedana' not found"
        echo "Install Cedana or use --baseline-only"
        exit 1
    fi
    echo "✓ RuntimeClass 'cedana' exists"
fi

# Check for nvidia runtime class (for baseline)
if [ $CEDANA_ONLY -eq 0 ]; then
    if ! kubectl get runtimeclass nvidia &>/dev/null; then
        echo "⚠ RuntimeClass 'nvidia' not found, baseline will use no runtimeClassName"
        BASELINE_RUNTIME=""
    else
        echo "✓ RuntimeClass 'nvidia' exists (will use for baseline)"
        BASELINE_RUNTIME="nvidia"
    fi
fi

# Check for required tools
for cmd in bc jq; do
    if ! command -v $cmd &>/dev/null; then
        echo "ERROR: Required command '$cmd' not found"
        exit 1
    fi
done

# Create namespace
echo ""
if ! kubectl get namespace "$NAMESPACE" &>/dev/null; then
    echo "Creating namespace $NAMESPACE..."
    kubectl create namespace "$NAMESPACE"
fi
echo "✓ Namespace ready: $NAMESPACE"

# Check for PVC if workload needs it
if grep -q "persistentVolumeClaim" "$WORKLOAD"; then
    PVC_NAME=$(grep -A1 "persistentVolumeClaim" "$WORKLOAD" | grep "claimName" | awk '{print $2}')
    if [ -n "$PVC_NAME" ]; then
        if ! kubectl get pvc "$PVC_NAME" -n "$NAMESPACE" &>/dev/null; then
            echo "Creating PVC $PVC_NAME..."
            kubectl apply -n "$NAMESPACE" -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: $PVC_NAME
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 100Gi
EOF
        fi
        echo "✓ PVC ready: $PVC_NAME"
    fi
fi

# Create profile directory if profiling enabled
if [ $PROFILE_ENABLED -eq 1 ]; then
    mkdir -p "$PROFILE_DIR"
    echo "✓ Profiling enabled, output: $PROFILE_DIR"
fi

#######################################
# Prepare workload
#######################################

echo ""
echo "========================================"
echo "Configuration"
echo "========================================"
echo "Workload:    $WORKLOAD"
echo "Samples:     $SAMPLES per variant"
echo "Timeout:     ${TIMEOUT}s"
echo "Namespace:   $NAMESPACE"
if [ -n "$EPOCHS" ]; then
    echo "Epochs:      $EPOCHS (override)"
fi
if [ $INTERLEAVE -eq 1 ]; then
    echo "Mode:        Interleaved"
else
    echo "Mode:        Sequential"
fi
if [ $PROFILE_ENABLED -eq 1 ]; then
    echo "Profiling:   Enabled ($PROFILE_DIR)"
fi
echo "Timing:      Container runtime (excludes scheduling/pull)"
echo "========================================"
echo ""

# Prepare base spec with epochs override if specified
BASE_SPEC="$WORKLOAD"
if [ -n "$EPOCHS" ]; then
    BASE_SPEC=$(set_epochs "$WORKLOAD" "$EPOCHS")
    echo "Applied epochs override: $EPOCHS"
fi

# Update namespace in spec
sed -i "s/namespace:.*/namespace: $NAMESPACE/" "$BASE_SPEC"

#######################################
# Run measurements
#######################################

declare -a BASELINE_TIMES
declare -a CEDANA_TIMES

run_baseline() {
    local run_num=$1
    echo ""
    echo "[Baseline Run $run_num/$SAMPLES]"

    local spec
    spec=$(set_unique_name "$BASE_SPEC" "perf-baseline")

    if [ -n "$BASELINE_RUNTIME" ]; then
        spec=$(set_runtime_class "$spec" "$BASELINE_RUNTIME")
        echo "  RuntimeClass: $BASELINE_RUNTIME"
    else
        spec=$(set_runtime_class "$spec" "")
        echo "  RuntimeClass: (none)"
    fi

    # Add profiling env and wait if enabled (native needs LD_PRELOAD)
    if [ $PROFILE_ENABLED -eq 1 ]; then
        spec=$(set_profiling_env_native "$spec")
        spec=$(set_profiling_wait "$spec" 60)
    fi

    local profile_subdir=""
    if [ $PROFILE_ENABLED -eq 1 ]; then
        profile_subdir="$PROFILE_DIR/baseline-$run_num"
    fi

    local duration
    duration=$(measure_pod_completion "$spec" "$TIMEOUT" "$NAMESPACE" "$PROFILE_ENABLED" "$profile_subdir")

    if [ "$duration" != "-1" ]; then
        echo "  Duration: ${duration}s"
        BASELINE_TIMES+=("$duration")
    else
        echo "  FAILED"
    fi

    # Cleanup temp spec
    rm -f "$spec" 2>/dev/null
}

run_cedana() {
    local run_num=$1
    echo ""
    echo "[Cedana Run $run_num/$SAMPLES]"

    local spec
    spec=$(set_unique_name "$BASE_SPEC" "perf-cedana")
    spec=$(set_runtime_class "$spec" "cedana")
    echo "  RuntimeClass: cedana"

    # Add profiling env and wait if enabled
    if [ $PROFILE_ENABLED -eq 1 ]; then
        spec=$(set_profiling_env "$spec")
        spec=$(set_profiling_wait "$spec" 60)
    fi

    local profile_subdir=""
    if [ $PROFILE_ENABLED -eq 1 ]; then
        profile_subdir="$PROFILE_DIR/cedana-$run_num"
    fi

    local duration
    duration=$(measure_pod_completion "$spec" "$TIMEOUT" "$NAMESPACE" "$PROFILE_ENABLED" "$profile_subdir")

    if [ "$duration" != "-1" ]; then
        echo "  Duration: ${duration}s"
        CEDANA_TIMES+=("$duration")
    else
        echo "  FAILED"
    fi

    # Cleanup temp spec
    rm -f "$spec" 2>/dev/null
}

echo "Starting performance measurements..."

if [ $INTERLEAVE -eq 1 ]; then
    # Interleaved: baseline, cedana, baseline, cedana, ...
    for i in $(seq 1 $SAMPLES); do
        if [ $CEDANA_ONLY -eq 0 ]; then
            run_baseline $i
        fi
        if [ $BASELINE_ONLY -eq 0 ]; then
            run_cedana $i
        fi
    done
else
    # Sequential: all baseline, then all cedana
    if [ $CEDANA_ONLY -eq 0 ]; then
        for i in $(seq 1 $SAMPLES); do
            run_baseline $i
        done
    fi
    if [ $BASELINE_ONLY -eq 0 ]; then
        for i in $(seq 1 $SAMPLES); do
            run_cedana $i
        done
    fi
fi

#######################################
# Output results
#######################################

WORKLOAD_NAME=$(basename "$WORKLOAD")

if [ $JSON_OUTPUT -eq 1 ]; then
    output_json "$WORKLOAD_NAME" "$SAMPLES" BASELINE_TIMES CEDANA_TIMES "$JSON_FILE"
fi

# Always print human-readable summary
print_results "$WORKLOAD_NAME" "$SAMPLES" BASELINE_TIMES CEDANA_TIMES

# Print profiling summaries if enabled
if [ $PROFILE_ENABLED -eq 1 ]; then
    echo ""
    echo "========================================"
    echo "Profiling Data"
    echo "========================================"

    # Find and print all profiling files
    for profile_file in "$PROFILE_DIR"/*/*.log; do
        if [ -f "$profile_file" ]; then
            run_dir=$(dirname "$profile_file")
            run_dir=$(basename "$run_dir")
            print_profiling_summary "$profile_file" "$run_dir"
        fi
    done

    echo ""
    echo "Profiling data saved to: $PROFILE_DIR"

    # Save results summary to profile directory
    RESULTS_FILE="$PROFILE_DIR/results.txt"
    {
        echo "========================================"
        echo "Performance Results (Container Runtime)"
        echo "========================================"
        echo "Workload: $WORKLOAD_NAME"
        echo "Samples:  $SAMPLES"
        echo "Date:     $(date)"
        echo ""
        print_results "$WORKLOAD_NAME" "$SAMPLES" BASELINE_TIMES CEDANA_TIMES
    } > "$RESULTS_FILE"
    echo "Results saved to: $RESULTS_FILE"

    # Also save JSON if we have jq
    if command -v jq &>/dev/null; then
        JSON_RESULTS_FILE="$PROFILE_DIR/results.json"
        output_json "$WORKLOAD_NAME" "$SAMPLES" BASELINE_TIMES CEDANA_TIMES "$JSON_RESULTS_FILE"
    fi
fi

echo ""
echo "Performance test completed."
