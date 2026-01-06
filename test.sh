#!/bin/bash
set -e

# Make sure GPU tests are enabled and set up your environment
export GPU=1
export PROVIDER=generic

# Choose which tests to run by setting TAGS:
# Single tag set (AND logic):
#   TAGS="k8s,gpu" - All GPU tests (basic + large)
#   TAGS="k8s,gpu,!large" - Only basic GPU tests (exclude large)
#   TAGS="k8s,gpu,llm" - Only LLM inference tests
#   TAGS="k8s,gpu,training" - Only training tests
#
# Multiple tag sets (OR logic) - separate with semicolon:
#   TAGS="k8s,gpu,llm;k8s,gpu,training" - LLM OR training tests
#   TAGS="k8s,gpu,tensorflow;k8s,gpu,deepspeed" - TensorFlow OR DeepSpeed
#
export TAGS="k8s,gpu,training"

export PARALLELISM=1 # Reduced to 1 to avoid GPU contention and make logs easier to follow

# Skip helm install/uninstall since you already have Cedana installed
export SKIP_HELM=1

# Set to your existing Cedana namespace (note: it's cedana-systems with an 's')
export CEDANA_NAMESPACE="cedana-systems"

# The test script will auto-detect the cluster ID from the cedana-config configmap
# Current cluster ID: d7fb23b9-d91c-41a1-9ccd-a4cc98dad0d1
# Uncomment to skip auto-detection:
# export CLUSTER_ID="d7fb23b9-d91c-41a1-9ccd-a4cc98dad0d1"

# Set test namespace (where test pods will be created)
export NAMESPACE="test"

# Optional: Set your cluster name (for logging purposes)
export CLUSTER_NAME="gpu-test-cluster-$(date +%s)"

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
echo "Running GPU Tests with Tags"
echo "======================================="
echo "GPU: $GPU"
echo "PROVIDER: $PROVIDER"
echo "TAGS: $TAGS"
echo "SKIP_HELM: $SKIP_HELM"
echo "PARALLELISM: $PARALLELISM"
echo "CEDANA_NAMESPACE: $CEDANA_NAMESPACE"
echo "NAMESPACE: $NAMESPACE"
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
    -r test/k8s

TEST_EXIT_CODE=$?

echo ""
echo "======================================="
echo "Test run completed (exit code: $TEST_EXIT_CODE)"
echo "======================================="

exit $TEST_EXIT_CODE
