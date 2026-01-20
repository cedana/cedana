#!/bin/bash
set -e

# Wrapper script that runs through all Kubernetes tests.
# Use the TAG logic to specify workloads to run through.
# All workloads are specified in k8s/*.bats files.

#######################################
# Tag Discovery and Interactive Setup #
#######################################

show_help() {
    cat <<EOF
Usage: $0 [OPTIONS]

Run Kubernetes tests with tag filtering.

OPTIONS:
    -t, --tags TAGS     Comma-separated tags to filter tests (default: k8s)
                        Use semicolon for OR logic: "k8s,gpu;k8s,cpu"
                        Use ! to exclude: "k8s,gpu,!large"
    -l, --list-tags     List all available tags from bats files and exit
    -i, --interactive   Interactive mode to select tags
    -g, --gpu           Enable GPU tests (sets GPU=1)
    -p, --parallelism N Number of parallel test jobs (default: 1)
    -n, --namespace NS  Test namespace (default: test)
    -h, --help          Show this help message

EXAMPLES:
    $0                          # Run all k8s tests
    $0 -t 'k8s,gpu'             # Run GPU tests
    $0 -t 'k8s,!gpu'            # Run CPU-only tests (exclude GPU)
    $0 -t 'k8s,gpu,!large'      # Run GPU tests excluding large ones
    $0 -t 'k8s,gpu,training'    # Run GPU training tests
    $0 -i                       # Interactive tag selection
    $0 -l                       # List all available tags

TAG LOGIC:
    - Comma separates tags with AND logic: 'k8s,gpu' = k8s AND gpu
    - Semicolon separates tag sets with OR logic: 'k8s,gpu;k8s,cpu' = (k8s AND gpu) OR (k8s AND cpu)
    - Exclamation excludes tags: 'k8s,gpu,!large' = k8s AND gpu AND NOT large

NOTE: Use single quotes when using '!' to exclude tags, to avoid bash history expansion.
EOF
}

# Extract all unique tags from bats files
get_all_tags() {
    local tags=()
    local script_dir
    script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

    # Extract file_tags
    while IFS= read -r line; do
        # Parse "# bats file_tags=tag1,tag2,tag3"
        if [[ $line =~ file_tags=(.+) ]]; then
            IFS=',' read -ra file_tags <<< "${BASH_REMATCH[1]}"
            tags+=("${file_tags[@]}")
        fi
    done < <(grep -h "bats file_tags=" "$script_dir"/k8s/*.bats 2>/dev/null || true)

    # Extract test_tags
    while IFS= read -r line; do
        # Parse "# bats test_tags=tag1,tag2,tag3"
        if [[ $line =~ test_tags=(.+) ]]; then
            IFS=',' read -ra test_tags <<< "${BASH_REMATCH[1]}"
            tags+=("${test_tags[@]}")
        fi
    done < <(grep -h "bats test_tags=" "$script_dir"/k8s/*.bats 2>/dev/null || true)

    # Sort and deduplicate
    printf '%s\n' "${tags[@]}" | sort -u
}

list_tags() {
    echo "Available tags from k8s/*.bats files:"
    echo ""
    echo "FILE TAGS (apply to entire files):"
    echo "  k8s, kubernetes, gpu, large"
    echo ""
    echo "TEST TAGS (apply to individual tests):"
    local tags
    tags=$(get_all_tags)
    echo "$tags" | tr '\n' ' '
    echo ""
    echo ""
    echo "Common tag combinations:"
    echo "  k8s                    - All k8s tests (default)"
    echo "  k8s,gpu                - All GPU tests"
    echo "  k8s,gpu,!large         - GPU tests without large/slow ones"
    echo "  k8s,gpu,training       - GPU training workloads"
    echo "  k8s,gpu,inference      - GPU inference workloads"
    echo "  k8s,gpu,multi          - Multi-GPU tests"
    echo "  k8s,samples            - All sample workload tests"
}

interactive_select() {
    echo "=== Interactive Tag Selection ==="
    echo ""

    # Get all unique tags
    local all_tags
    all_tags=$(get_all_tags)

    # Convert to array
    local -a tag_array
    mapfile -t tag_array <<< "$all_tags"

    echo "Available tags:"
    local i=1
    for tag in "${tag_array[@]}"; do
        printf "  %2d) %s\n" "$i" "$tag"
        ((i++))
    done
    echo ""

    # Preset options
    echo "Preset options:"
    echo "  a) All k8s tests (k8s)"
    echo "  b) GPU tests (k8s,gpu)"
    echo "  c) GPU tests without large (k8s,gpu,!large)"
    echo "  d) CPU tests only (k8s,!gpu)"
    echo "  e) Training tests (k8s,gpu,training)"
    echo "  f) Inference tests (k8s,gpu,inference)"
    echo "  g) Multi-GPU tests (k8s,gpu,multi)"
    echo ""

    read -rp "Enter preset letter, tag numbers (comma-separated), or custom tags: " selection

    case "$selection" in
        a) TAGS="k8s" ;;
        b) TAGS="k8s,gpu" ;;
        c) TAGS="k8s,gpu,!large" ;;
        d) TAGS="k8s,!gpu" ;;
        e) TAGS="k8s,gpu,training" ;;
        f) TAGS="k8s,gpu,inference" ;;
        g) TAGS="k8s,gpu,multi" ;;
        *)
            # Check if it's numbers or custom tags
            if [[ "$selection" =~ ^[0-9,\ ]+$ ]]; then
                # Numbers - build tag list
                local selected_tags="k8s"
                IFS=', ' read -ra nums <<< "$selection"
                for num in "${nums[@]}"; do
                    if [[ $num -ge 1 && $num -le ${#tag_array[@]} ]]; then
                        selected_tags="$selected_tags,${tag_array[$((num-1))]}"
                    fi
                done
                TAGS="$selected_tags"
            else
                # Custom tags - use as-is, prepend k8s if not present
                if [[ "$selection" != *"k8s"* ]]; then
                    TAGS="k8s,$selection"
                else
                    TAGS="$selection"
                fi
            fi
            ;;
    esac

    echo ""
    echo "Selected tags: $TAGS"
    read -rp "Proceed? [Y/n] " confirm
    if [[ "$confirm" =~ ^[Nn] ]]; then
        echo "Aborted."
        exit 0
    fi
}

# Default values
TAGS="${TAGS:-k8s}"
GPU="${GPU:-0}"
PARALLELISM="${PARALLELISM:-1}"
NAMESPACE="${NAMESPACE:-test}"
INTERACTIVE=0
LIST_TAGS=0

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -t|--tags)
            TAGS="$2"
            shift 2
            ;;
        -l|--list-tags)
            LIST_TAGS=1
            shift
            ;;
        -i|--interactive)
            INTERACTIVE=1
            shift
            ;;
        -g|--gpu)
            GPU=1
            shift
            ;;
        -p|--parallelism)
            PARALLELISM="$2"
            shift 2
            ;;
        -n|--namespace)
            NAMESPACE="$2"
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

# Handle list tags
if [[ $LIST_TAGS -eq 1 ]]; then
    list_tags
    exit 0
fi

# Handle interactive mode
if [[ $INTERACTIVE -eq 1 ]]; then
    interactive_select
fi

# Auto-enable GPU if gpu tag is present
if [[ "$TAGS" == *"gpu"* && "$TAGS" != *"!gpu"* ]]; then
    GPU=1
fi

export GPU
export TAGS
export PARALLELISM
export NAMESPACE
export PROVIDER=generic

# Skip helm install/uninstall since you already have Cedana installed
export SKIP_HELM="${SKIP_HELM:-1}"

# Set to your existing Cedana namespace
export CEDANA_NAMESPACE="${CEDANA_NAMESPACE:-cedana-systems}"

# The test script will auto-detect the cluster ID from the cedana-config configmap
# Uncomment to skip auto-detection:
# export CLUSTER_ID="your-cluster-id"

# Optional: Set your cluster name (for logging purposes)
export CLUSTER_NAME="${CLUSTER_NAME:-k8s-test-cluster-$(date +%s)}"

# Enable verbose bats output
export BATS_NO_FAIL_FOCUS_RUN=1
export BATS_TEST_RETRIES=0

# Set log level for more verbose output
export CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-debug}"

# Set samples dir
export SAMPLES_DIR="$(pwd)/cedana-samples/kubernetes"
# Ensure samples directory exists
if [ ! -d "$SAMPLES_DIR" ]; then
    echo "ERROR: Samples directory not found at $SAMPLES_DIR, cloning..."
    git clone https://github.com/cedana/cedana-samples.git
fi

# === Propagator API Configuration ===
# These need to be set for the tests to communicate with the Cedana propagator
# If not already set in your environment, we'll fetch them from the cluster
if [ -z "$CEDANA_URL" ]; then
    CEDANA_URL=$(kubectl get cm cedana-config -n "$CEDANA_NAMESPACE" -o jsonpath='{.data.url}' 2>/dev/null)
    export CEDANA_URL
fi

if [ -z "$CEDANA_AUTH_TOKEN" ]; then
    CEDANA_AUTH_TOKEN=$(kubectl get secret cedana-secrets -n "$CEDANA_NAMESPACE" -o jsonpath='{.data.auth-token}' 2>/dev/null | base64 -d)
    export CEDANA_AUTH_TOKEN
fi

# Pre-flight checks
echo "======================================="
echo "Pre-flight Checks"
echo "======================================="

# Check kubectl connectivity
if ! kubectl cluster-info &>/dev/null; then
    echo "ERROR: Cannot connect to Kubernetes cluster"
    exit 1
fi
echo "✓ Connected to cluster: $(kubectl config current-context)"

# Check Cedana namespace
if ! kubectl get namespace "$CEDANA_NAMESPACE" &>/dev/null; then
    echo "ERROR: Cedana namespace '$CEDANA_NAMESPACE' not found"
    exit 1
fi
echo "✓ Cedana namespace exists: $CEDANA_NAMESPACE"

# Check Cedana pods
CEDANA_PODS=$(kubectl get pods -n "$CEDANA_NAMESPACE" --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l)
if [ "$CEDANA_PODS" -eq 0 ]; then
    echo "ERROR: No running Cedana pods found in namespace '$CEDANA_NAMESPACE'"
    exit 1
fi
echo "✓ Cedana pods running: $CEDANA_PODS"

# Get cluster ID
DETECTED_CLUSTER_ID=$(kubectl get cm cedana-config -n "$CEDANA_NAMESPACE" -o jsonpath='{.data.cluster-id}' 2>/dev/null)
if [ -n "$DETECTED_CLUSTER_ID" ]; then
    echo "✓ Cluster ID detected: $DETECTED_CLUSTER_ID"
else
    echo "WARNING: Could not detect cluster ID from configmap"
fi

# Check propagator credentials
if [ -z "$CEDANA_URL" ]; then
    echo "ERROR: CEDANA_URL is not set"
    exit 1
fi
echo "✓ CEDANA_URL: $CEDANA_URL"

if [ -z "$CEDANA_AUTH_TOKEN" ]; then
    echo "ERROR: CEDANA_AUTH_TOKEN is not set"
    exit 1
fi
echo "✓ CEDANA_AUTH_TOKEN: ${CEDANA_AUTH_TOKEN:0:20}... (${#CEDANA_AUTH_TOKEN} chars)"

# Test propagator connectivity
echo -n "✓ Testing propagator connectivity... "
PROPAGATOR_URL="${CEDANA_URL%/v1}"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $CEDANA_AUTH_TOKEN" "$PROPAGATOR_URL/v2/user" 2>/dev/null || echo "000")
if [ "$HTTP_CODE" = "200" ]; then
    echo "OK (HTTP $HTTP_CODE)"
elif [ "$HTTP_CODE" = "000" ]; then
    echo "FAILED (connection error)"
    echo "ERROR: Cannot reach propagator at $PROPAGATOR_URL"
    echo "Please check your network connectivity and CEDANA_URL"
    exit 1
else
    echo "WARNING (HTTP $HTTP_CODE)"
    echo "WARNING: Got unexpected HTTP code from propagator"
fi

echo ""
echo "======================================="
echo "Setting up Test Namespace"
echo "======================================="

# Create test namespace if it doesn't exist
if ! kubectl get namespace "$NAMESPACE" &>/dev/null; then
    echo "Creating namespace $NAMESPACE..."
    kubectl create namespace "$NAMESPACE"
fi
echo "✓ Test namespace ready: $NAMESPACE"

# Create PVC required by training workloads (dgtest-pvc)
if ! kubectl get pvc dgtest-pvc -n "$NAMESPACE" &>/dev/null; then
    echo "Creating PVC dgtest-pvc in namespace $NAMESPACE..."
    kubectl apply -n "$NAMESPACE" -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: dgtest-pvc
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 50Gi
EOF
fi
echo "✓ PVC dgtest-pvc ready"

echo ""
echo "======================================="
echo "Running K8s Tests with Tags"
echo "======================================="
echo "TAGS: $TAGS"
echo "GPU: $GPU"
echo "PROVIDER: $PROVIDER"
echo "PARALLELISM: $PARALLELISM"
echo "SKIP_HELM: $SKIP_HELM"
echo "CEDANA_NAMESPACE: $CEDANA_NAMESPACE"
echo "TEST_NAMESPACE: $NAMESPACE"
echo "======================================="
echo ""

# Run tests with tag filtering across all GPU test files
# Using tap13 formatter for better output with timing
# Build the filter-tags arguments (supports OR logic with semicolon separator)
FILTER_ARGS=""
IFS=';' read -ra TAG_SETS <<<"$TAGS"
for tag_set in "${TAG_SETS[@]}"; do
    FILTER_ARGS="$FILTER_ARGS --filter-tags $tag_set"
done

bats \
    $FILTER_ARGS \
    --jobs "$PARALLELISM" \
    --verbose-run \
    --print-output-on-failure \
    --timing \
    --formatter tap13 \
    -r k8s

TEST_EXIT_CODE=$?

echo ""
echo "======================================="
echo "Test run completed (exit code: $TEST_EXIT_CODE)"
echo "======================================="

exit $TEST_EXIT_CODE
