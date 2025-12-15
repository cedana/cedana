#!/usr/bin/env bats

################################################################################
# Unified Kubernetes Tests
################################################################################
#
# This test file runs against any Kubernetes cluster using the provider abstraction.
# Select provider via K8S_PROVIDER environment variable.
#
# Providers:
#   generic  - Pre-configured cluster (default) - requires kubectl to be configured
#   aws/eks  - Amazon EKS - requires AWS credentials
#   gcp/gke  - Google GKE - requires GCP credentials
#   nebius   - Nebius Cloud - requires Nebius credentials, creates GPU nodegroups
#   k3s      - Local K3s - creates fresh cluster
#
# Environment variables:
#   K8S_PROVIDER              - Provider to use (default: generic)
#   KUBECONFIG                - Path to kubeconfig (default: ~/.kube/config)
#   CLUSTER_NAME              - Name for registering cluster with propagator (default: auto-generated)
#   CLUSTER_ID                - If set, skip cluster registration and use this ID
#   SKIP_HELM_INSTALL         - If set to "1", skip helm install (assumes Cedana is already installed)
#   SKIP_HELM_UNINSTALL       - If set to "1", skip helm uninstall on teardown
#   GPU_ENABLED               - If set to "1", run GPU tests
#   CEDANA_CHECKPOINT_DIR     - Checkpoint storage location (default: cedana://ci)
#   CEDANA_CHECKPOINT_COMPRESSION - Compression algorithm (default: lz4)
#   CEDANA_NAMESPACE          - Namespace for Cedana components (default: cedana-systems)
#   NAMESPACE                 - Namespace for test pods (default: test)
#   SAMPLES_DIR               - Path to cedana-samples/kubernetes (default: auto-detect)
#   TEST_FILTER               - Comma-separated list of sample filenames to test (default: all)
#   SKIP_RESTORE              - If set to "1", skip restore tests (only deploy + checkpoint)
#
# Usage examples:
#   # Run against a pre-configured cluster with Cedana already installed
#   SKIP_HELM_INSTALL=1 CLUSTER_ID="your-cluster-id" bats test/k8s/k8s.bats
#
#   # Run against EKS
#   K8S_PROVIDER=aws bats test/k8s/k8s.bats
#
#   # Run with GPU tests enabled
#   GPU_ENABLED=1 bats test/k8s/k8s.bats
#
#   # Run specific samples only
#   TEST_FILTER="counting.yaml,cuda-vector-add.yaml" bats test/k8s/k8s.bats
#
#   # Run on Nebius with GPU
#   K8S_PROVIDER=nebius GPU_ENABLED=1 bats test/k8s/k8s.bats
#

# bats file_tags=k8s,kubernetes

################################################################################
# Setup and Configuration
################################################################################

# Defaults for remote checkpoint storage
export CEDANA_CHECKPOINT_DIR=${CEDANA_CHECKPOINT_DIR:-cedana://ci}
export CEDANA_CHECKPOINT_COMPRESSION=${CEDANA_CHECKPOINT_COMPRESSION:-lz4}

# Load helpers
load ../helpers/utils
load ../helpers/daemon # required for config env vars
load ../helpers/providers/provider # auto-loads correct provider
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

# Generate cluster name if not provided
if [ -z "$CLUSTER_NAME" ]; then
    CLUSTER_NAME="test-${K8S_PROVIDER}-$(unix_nano)"
fi
export CLUSTER_NAME
export CLUSTER_ID
export NAMESPACE="${NAMESPACE:-test}"
export CEDANA_NAMESPACE="${CEDANA_NAMESPACE:-cedana-systems}"

# Auto-detect or clone samples directory
if [ -z "$SAMPLES_DIR" ]; then
    if [ -d "../cedana-samples/kubernetes" ]; then
        SAMPLES_DIR="../cedana-samples/kubernetes"
    elif [ -d "/cedana-samples/kubernetes" ]; then
        SAMPLES_DIR="/cedana-samples/kubernetes"
    elif [ -d "/tmp/cedana-samples/kubernetes" ]; then
        SAMPLES_DIR="/tmp/cedana-samples/kubernetes"
    else
        # Clone cedana-samples repo to /tmp
        if git clone --depth 1 https://github.com/cedana/cedana-samples.git /tmp/cedana-samples 2>/dev/null; then
            SAMPLES_DIR="/tmp/cedana-samples/kubernetes"
        else
            SAMPLES_DIR=""
        fi
    fi
fi
export SAMPLES_DIR

################################################################################
# Helper Functions
################################################################################

# Check if a sample should be tested based on TEST_FILTER
should_test_sample() {
    local filename="$1"
    if [ -z "$TEST_FILTER" ]; then
        return 0  # No filter, test everything
    fi
    echo "$TEST_FILTER" | grep -q "$filename"
}

# Check if sample is GPU-based (from metadata.json)
is_gpu_sample() {
    local filename="$1"
    if [ -z "$SAMPLES_DIR" ] || [ ! -f "$SAMPLES_DIR/metadata.json" ]; then
        echo "$filename" | grep -qi "cuda\|gpu"
        return $?
    fi
    local type
    type=$(jq -r --arg f "$filename" '.[] | select(.filename == $f) | .type' "$SAMPLES_DIR/metadata.json" 2>/dev/null)
    [ "$type" = "gpu" ]
}

# Get available GPU count from any schedulable node
get_available_gpus() {
    local gpu_count
    gpu_count=$(kubectl get nodes -o json | jq '[.items[].status.allocatable["nvidia.com/gpu"] // "0" | tonumber] | add' 2>/dev/null)
    echo "${gpu_count:-0}"
}

# Count total GPUs required by a spec file
get_required_gpus() {
    local spec_file="$1"
    local gpu_count
    gpu_count=$(grep -o "nvidia.com/gpu.*[0-9]" "$spec_file" 2>/dev/null | grep -o "[0-9]*" | awk '{sum+=$1} END {print sum}')
    echo "${gpu_count:-0}"
}

# Prepare a sample spec for testing (change namespace, etc.)
prepare_sample_spec() {
    local source_file="$1"
    local test_namespace="$2"
    local unique_id="$3"

    local temp_spec="/tmp/test-spec-${unique_id}.yaml"

    sed -e "s/namespace: default/namespace: $test_namespace/g" \
        -e "s/namespace: .*/namespace: $test_namespace/g" \
        "$source_file" > "$temp_spec"

    if ! grep -q "namespace:" "$temp_spec"; then
        sed -i '/^metadata:/a\  namespace: '"$test_namespace"'' "$temp_spec"
    fi

    echo "$temp_spec"
}

# Get pod name from a created resource
get_created_pod_name() {
    local spec_file="$1"
    local namespace="$2"
    local timeout="${3:-60}"

    local name
    name=$(grep -E "^\s*name:" "$spec_file" | head -1 | awk '{print $2}' | tr -d '"')
    local generate_name
    generate_name=$(grep -E "^\s*generateName:" "$spec_file" | head -1 | awk '{print $2}' | tr -d '"')

    if [ -n "$name" ]; then
        echo "$name"
        return 0
    elif [ -n "$generate_name" ]; then
        for i in $(seq 1 $timeout); do
            local pod
            pod=$(kubectl get pods -n "$namespace" --no-headers 2>/dev/null | grep "^${generate_name}" | head -1 | awk '{print $1}')
            if [ -n "$pod" ]; then
                echo "$pod"
                return 0
            fi
            sleep 1
        done
        error_log "Timeout waiting for pod with prefix $generate_name"
        return 1
    else
        error_log "Could not determine pod name from spec"
        return 1
    fi
}

# Run a complete test cycle for a sample: Deploy -> Wait -> Checkpoint -> Restore -> Cleanup
run_sample_test() {
    local spec_file="$1"
    local namespace="$2"
    local is_gpu="$3"
    local skip_restore="${4:-0}"

    local wait_time=5
    local pod_timeout=300
    if [ "$is_gpu" = "1" ]; then
        wait_time=60
        pod_timeout=900
    fi

    local container_name
    container_name=$(grep -A1 "containers:" "$spec_file" | grep "name:" | head -1 | awk '{print $3}' | tr -d '"' | tr -d "'")
    debug_log "Container name from spec: $container_name"

    # Deploy
    debug_log "Deploying from $spec_file..."
    if grep -q "generateName:" "$spec_file"; then
        kubectl create -f "$spec_file"
    else
        kubectl apply -f "$spec_file"
    fi

    sleep "$wait_time"

    # Get pod name
    local pod_name
    pod_name=$(get_created_pod_name "$spec_file" "$namespace" 60)
    if [ -z "$pod_name" ]; then
        error_log "Failed to get pod name"
        return 1
    fi
    debug_log "Pod name: $pod_name"

    # Wait for running
    debug_log "Waiting for pod to be running..."
    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$pod_name" --timeout="${pod_timeout}s" -n "$namespace" || {
        error_log "Pod failed to reach Running state"
        kubectl describe pod "$pod_name" -n "$namespace" >&3 || true
        kubectl delete pod "$pod_name" -n "$namespace" --wait=false || true
        return 1
    }

    # Checkpoint
    debug_log "Checkpointing pod..."
    local pod_id
    pod_id=$(get_pod_id "$pod_name" "$namespace")

    local checkpoint_output
    checkpoint_output=$(checkpoint_pod "$pod_id")
    local checkpoint_status=$?

    if [ $checkpoint_status -ne 0 ]; then
        error_log "Checkpoint failed: $checkpoint_output"
        kubectl delete pod "$pod_name" -n "$namespace" --wait=true || true
        return 1
    fi

    local action_id="$checkpoint_output"
    validate_action_id "$action_id" || {
        error_log "Invalid action ID: $action_id"
        kubectl delete pod "$pod_name" -n "$namespace" --wait=true || true
        return 1
    }

    poll_action_status "$action_id" "checkpoint" || {
        error_log "Checkpoint polling failed"
        kubectl delete pod "$pod_name" -n "$namespace" --wait=true || true
        return 1
    }

    debug_log "Checkpoint completed successfully"

    # Restore (unless skipped)
    if [ "$skip_restore" != "1" ]; then
        debug_log "Deleting original pod before restore..."
        kubectl delete pod "$pod_name" -n "$namespace" --wait=true || true
        pod_name=""

        debug_log "Restoring pod..."

        local restore_output
        restore_output=$(restore_pod "$action_id" "$CLUSTER_ID")
        local restore_status=$?

        if [ $restore_status -ne 0 ]; then
            error_log "Restore failed: $restore_output"
            return 1
        fi

        local restore_action_id="$restore_output"
        validate_action_id "$restore_action_id" || {
            error_log "Invalid restore action ID: $restore_action_id"
            return 1
        }

        local restored_pod=""
        local wait_elapsed=0
        local wait_timeout=120
        while [ -z "$restored_pod" ] && [ $wait_elapsed -lt $wait_timeout ]; do
            restored_pod=$(get_restored_pod "$namespace" "$container_name" 2>/dev/null) || true
            if [ -z "$restored_pod" ]; then
                sleep 2
                ((wait_elapsed += 2))
            fi
        done

        if [ -z "$restored_pod" ]; then
            error_log "Failed to find restored pod after ${wait_timeout}s"
            for rp in $(list_restored_pods "$namespace"); do
                kubectl delete pod "$rp" -n "$namespace" --wait=false 2>/dev/null || true
            done
            return 1
        fi

        debug_log "Restored pod: $restored_pod"

        validate_pod "$namespace" "$restored_pod" 30s || {
            error_log "Restored pod validation failed"
            kubectl delete pod "$restored_pod" -n "$namespace" --wait=false 2>/dev/null || true
            return 1
        }

        debug_log "Restore completed successfully"
        kubectl delete pod "$restored_pod" -n "$namespace" --wait=true || true
    fi

    # Cleanup original pod (if not already deleted)
    if [ -n "$pod_name" ]; then
        kubectl delete pod "$pod_name" -n "$namespace" --wait=true || true
    fi

    return 0
}

################################################################################
# File Setup/Teardown
################################################################################

setup_file() {
    # Setup cluster using provider
    setup_cluster

    # Verify kubectl connectivity
    if ! kubectl cluster-info &>/dev/null; then
        error_log "Cannot connect to Kubernetes cluster after provider setup."
        return 1
    fi

    debug_log "Connected to cluster: $(kubectl config current-context)"
    debug_log "Provider: $K8S_PROVIDER"
    debug_log "Samples directory: ${SAMPLES_DIR:-not set}"

    # Install Cedana helm chart unless skipped
    if [ "${SKIP_HELM_INSTALL:-0}" != "1" ]; then
        # Clean up any leftover resources from previous test runs
        # This prevents helm install failures due to orphaned cluster-scoped resources
        debug_log "Cleaning up any leftover cedana resources..."
        kubectl delete pods -n "$CEDANA_NAMESPACE" --field-selector=status.phase=Failed --ignore-not-found=true 2>/dev/null || true
        kubectl delete pods -n "$CEDANA_NAMESPACE" -l app.kubernetes.io/component=uninstaller --ignore-not-found=true 2>/dev/null || true

        # If Cedana is already installed, uninstall it first to ensure clean state
        # This is especially important for persistent clusters (EKS, GKE, Nebius)
        if helm status cedana -n "$CEDANA_NAMESPACE" &>/dev/null; then
            debug_log "Found existing Cedana installation, uninstalling first..."
            helm_uninstall_cedana "$CEDANA_NAMESPACE" || true
            # Also clean up any orphaned resources after uninstall
            kubectl delete pods -n "$CEDANA_NAMESPACE" --all --ignore-not-found=true 2>/dev/null || true
            kubectl delete pvc -n "$CEDANA_NAMESPACE" --all --ignore-not-found=true 2>/dev/null || true
            # Wait for namespace to be clean
            sleep 5
        fi

        if [ -z "$CLUSTER_ID" ]; then
            debug_log "Registering cluster '$CLUSTER_NAME' with propagator..."
            CLUSTER_ID=$(register_cluster "$CLUSTER_NAME")
            export CLUSTER_ID
            debug_log "Cluster registered with ID: $CLUSTER_ID"
        else
            debug_log "Using provided cluster ID: $CLUSTER_ID"
        fi
        helm_install_cedana "$CLUSTER_ID" "$CEDANA_NAMESPACE"
        wait_for_ready "$CEDANA_NAMESPACE" 300
    else
        debug_log "Skipping helm install (SKIP_HELM_INSTALL=1)"
        if [ -z "$CLUSTER_ID" ]; then
            CLUSTER_ID=$(kubectl get cm cedana-config -n "$CEDANA_NAMESPACE" -o jsonpath='{.data.cluster-id}' 2>/dev/null)
            if [ -z "$CLUSTER_ID" ]; then
                error_log "SKIP_HELM_INSTALL=1 but no cedana-config configmap found. Please provide CLUSTER_ID."
                return 1
            fi
            export CLUSTER_ID
            debug_log "Using cluster ID from existing installation: $CLUSTER_ID"
        fi
        if kubectl get pods -n "$CEDANA_NAMESPACE" --no-headers 2>/dev/null | grep -q .; then
            wait_for_ready "$CEDANA_NAMESPACE" 300
        fi
    fi

    # Start tailing logs in background
    tail_all_logs $CEDANA_NAMESPACE 300 &
    TAIL_PID=$!

    # Create test namespace
    create_namespace "$NAMESPACE"
}

teardown_file() {
    # Stop log tailing
    if [ -n "$TAIL_PID" ]; then
        kill "$TAIL_PID" 2>/dev/null || true
    fi

    # Clean up test namespace
    delete_namespace "$NAMESPACE" --force

    # Clean up any leftover PVs from tests
    kubectl delete pv --all --wait=false 2>/dev/null || true

    # Uninstall helm chart unless skipped
    if [ "${SKIP_HELM_UNINSTALL:-0}" != "1" ] && [ "${SKIP_HELM_INSTALL:-0}" != "1" ]; then
        helm_uninstall_cedana $CEDANA_NAMESPACE
    else
        debug_log "Skipping helm uninstall"
    fi

    # Deregister cluster (only if we registered it)
    if [ -n "$CLUSTER_ID" ] && [ -z "${SKIP_CLUSTER_DEREGISTER:-}" ] && [ "${SKIP_HELM_INSTALL:-0}" != "1" ]; then
        deregister_cluster "$CLUSTER_ID"
    fi

    # Teardown cluster using provider
    teardown_cluster
}

teardown() {
    if [ "$DEBUG" != '1' ]; then
        error all_logs "$CEDANA_NAMESPACE" 120 1000
    fi
}

setup() {
    create_namespace "$NAMESPACE" 2>/dev/null || true
}

################################################################################
# Verification Tests
################################################################################

@test "Verify cluster and Cedana installation" {
    run kubectl get nodes
    [ "$status" -eq 0 ]
    [[ "$output" == *"Ready"* ]]

    kubectl get pods -n $CEDANA_NAMESPACE

    kubectl wait --for=condition=Ready pod -l app.kubernetes.io/instance=cedana -n $CEDANA_NAMESPACE --timeout=300s

    validate_propagator_connectivity
}

################################################################################
# Core CPU Tests
################################################################################

# bats test_tags=deploy,cpu
@test "Deploy a pod" {
    local name
    name=$(unix_nano)
    local script
    script=$(cat "$WORKLOADS"/date-loop.sh)
    local spec
    spec=$(cmd_pod_spec "$NAMESPACE" "$name" "alpine:latest" "$script")

    kubectl apply -f "$spec"

    sleep 5

    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=300s -n "$NAMESPACE"

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
}

# bats test_tags=dump,cpu
@test "Checkpoint a pod" {
    local name
    name=$(unix_nano)
    local script
    script=$(cat "$WORKLOADS"/date-loop.sh)
    local spec
    spec=$(cmd_pod_spec "$NAMESPACE" "$name" "alpine:latest" "$script")

    kubectl apply -f "$spec"

    sleep 5

    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=300s -n "$NAMESPACE"

    pod_id=$(get_pod_id "$name" "$NAMESPACE")
    run checkpoint_pod "$pod_id"
    [ "$status" -eq 0 ]

    if [ $status -eq 0 ]; then
        action_id=$output
        validate_action_id "$action_id"

        poll_action_status "$action_id" "checkpoint"
    fi

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
}

# bats test_tags=restore,cpu
@test "Restore a pod with original pod running" {
    local name
    name=$(unix_nano)
    local script
    script=$(cat "$WORKLOADS"/date-loop.sh)
    local spec
    spec=$(cmd_pod_spec "$NAMESPACE" "$name" "alpine:latest" "$script")

    kubectl apply -f "$spec"

    sleep 5

    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=300s -n "$NAMESPACE"

    pod_id=$(get_pod_id "$name" "$NAMESPACE")
    run checkpoint_pod "$pod_id"
    [ "$status" -eq 0 ]

    if [ $status -eq 0 ]; then
        action_id=$output
        validate_action_id "$action_id"

        poll_action_status "$action_id" "checkpoint"

        run restore_pod "$action_id" "$CLUSTER_ID"
        [ "$status" -eq 0 ]

        if [ $status -eq 0 ]; then
            action_id="$output"
            validate_action_id "$action_id"

            run wait_for_cmd 30 get_restored_pod "$NAMESPACE" "$name"
            [ "$status" -eq 0 ]

            if [ $status -eq 0 ]; then
                local restored_pod="$output"
                validate_pod "$NAMESPACE" "$restored_pod" 20s

                kubectl delete pod "$restored_pod" -n "$NAMESPACE" --wait=true
            fi
        fi
    fi

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
}

# bats test_tags=restore,cpu
@test "Restore a pod with original pod deleted" {
    local name
    name=$(unix_nano)
    local script
    script=$(cat "$WORKLOADS"/date-loop.sh)
    local spec
    spec=$(cmd_pod_spec "$NAMESPACE" "$name" "alpine:latest" "$script")

    kubectl apply -f "$spec"

    sleep 5

    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=300s -n "$NAMESPACE"

    pod_id=$(get_pod_id "$name" "$NAMESPACE")
    run checkpoint_pod "$pod_id"
    [ "$status" -eq 0 ]

    if [ $status -eq 0 ]; then
        action_id=$output
        validate_action_id "$action_id"

        poll_action_status "$action_id" "checkpoint"

        kubectl delete pod "$name" -n "$NAMESPACE" --wait=true

        run restore_pod "$action_id" "$CLUSTER_ID"
        [ "$status" -eq 0 ]

        if [ $status -eq 0 ]; then
            action_id="$output"
            validate_action_id "$action_id"

            run wait_for_cmd 30 get_restored_pod "$NAMESPACE" "$name"
            [ "$status" -eq 0 ]

            if [ $status -eq 0 ]; then
                local restored_pod="$output"
                validate_pod "$NAMESPACE" "$restored_pod" 20s

                kubectl delete pod "$restored_pod" -n "$NAMESPACE" --wait=true
            fi
        fi
    fi
}

# bats test_tags=restore,pvc
@test "Checkpoint and restore pod with PVC" {
    skip "Skipped until it supports running parallely with other tests"

    local pv_name="counting-pv"
    local pvc_name="counting-pvc"
    local pod_name="counting-pvc-pod"

    kubectl apply -f /cedana-samples/kubernetes/counting-persistent-volume.yaml -n "$NAMESPACE"

    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$pod_name" --timeout=300s -n "$NAMESPACE"

    pod_id=$(get_pod_id "$pod_name" "$NAMESPACE")
    run checkpoint_pod "$pod_id"
    [ "$status" -eq 0 ]

    if [ $status -eq 0 ]; then
        action_id=$output
        validate_action_id "$action_id"

        poll_action_status "$action_id" "checkpoint"

        kubectl delete pod "$pod_name" -n "$NAMESPACE" --wait=true

        run restore_pod "$action_id" "$CLUSTER_ID"
        [ "$status" -eq 0 ]

        if [ $status -eq 0 ]; then
            action_id="$output"
            validate_action_id "$action_id"

            run wait_for_cmd 30 get_restored_pod "$NAMESPACE" "$pod_name"
            [ "$status" -eq 0 ]

            if [ $status -eq 0 ]; then
                local restored_pod="$output"
                validate_pod "$NAMESPACE" "$restored_pod" 20s

                kubectl get pvc "$pvc_name" -n "$NAMESPACE" -o jsonpath='{.status.phase}'

                run kubectl get pod "$restored_pod" -n "$NAMESPACE" -o jsonpath='{.spec.volumes[*].persistentVolumeClaim.claimName}'
                [ "$status" -eq 0 ]
                [[ "$output" == *"$pvc_name"* ]]

                kubectl delete pod "$restored_pod" -n "$NAMESPACE" --wait=true
            fi
        fi
    fi

    run kubectl delete pvc "$pvc_name" -n "$NAMESPACE" --wait=true
    run kubectl delete pv "$pv_name" --wait=true
}

################################################################################
# GPU Tests
################################################################################

# bats test_tags=deploy,gpu
@test "Deploy a GPU pod" {
    if [ "${GPU_ENABLED:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU_ENABLED=1)"
    fi

    local name
    name=$(unix_nano)
    local script
    script=$'gpu_smr/vector_add'
    local spec
    spec=$(gpu_cmd_pod_spec "$NAMESPACE" "$name" "cedana/cedana-samples:cuda" "$script")

    kubectl apply -f "$spec"

    sleep 60

    kubectl get pod "$name" -n "$NAMESPACE" >&3
    kubectl describe pod "$name" -n "$NAMESPACE" >&3

    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=900s -n "$NAMESPACE"

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
}

# bats test_tags=dump,gpu
@test "Checkpoint a GPU pod" {
    if [ "${GPU_ENABLED:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU_ENABLED=1)"
    fi

    local name
    name=$(unix_nano)
    local script
    script=$'gpu_smr/vector_add'
    local spec
    spec=$(gpu_cmd_pod_spec "$NAMESPACE" "$name" "cedana/cedana-samples:cuda" "$script")

    kubectl apply -f "$spec"

    sleep 40

    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=900s -n "$NAMESPACE"

    pod_id=$(get_pod_id "$name" "$NAMESPACE")
    run checkpoint_pod "$pod_id"
    [ "$status" -eq 0 ]

    if [ $status -eq 0 ]; then
        action_id=$output
        validate_action_id "$action_id"

        poll_action_status "$action_id" "checkpoint"
    fi

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
}

# bats test_tags=restore,gpu
@test "Restore a GPU pod with original pod running" {
    if [ "${GPU_ENABLED:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU_ENABLED=1)"
    fi

    local name
    name=$(unix_nano)
    local script
    script=$'gpu_smr/vector_add'
    local spec
    spec=$(gpu_cmd_pod_spec "$NAMESPACE" "$name" "cedana/cedana-samples:cuda" "$script")

    kubectl apply -f "$spec"

    sleep 40

    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=900s -n "$NAMESPACE"

    pod_id=$(get_pod_id "$name" "$NAMESPACE")
    run checkpoint_pod "$pod_id"
    [ "$status" -eq 0 ]

    if [ $status -eq 0 ]; then
        action_id=$output
        validate_action_id "$action_id"

        poll_action_status "$action_id" "checkpoint"

        run restore_pod "$action_id" "$CLUSTER_ID"
        [ "$status" -eq 0 ]

        if [ $status -eq 0 ]; then
            action_id="$output"
            validate_action_id "$action_id"

            run wait_for_cmd 30 get_restored_pod "$NAMESPACE" "$name"
            [ "$status" -eq 0 ]

            if [ $status -eq 0 ]; then
                local restored_pod="$output"
                validate_pod "$NAMESPACE" "$restored_pod" 20s

                kubectl delete pod "$restored_pod" -n "$NAMESPACE" --wait=true
            fi
        fi
    fi

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
}

################################################################################
# Sample-Based Tests (CPU)
################################################################################

# bats test_tags=samples,cpu
@test "Sample: counting.yaml - Timestamp Logger" {
    if [ -z "$SAMPLES_DIR" ]; then
        skip "SAMPLES_DIR not set"
    fi
    if [ ! -f "$SAMPLES_DIR/counting.yaml" ]; then
        skip "counting.yaml not found"
    fi
    if ! should_test_sample "counting.yaml"; then
        skip "Filtered out by TEST_FILTER"
    fi

    local spec
    spec=$(prepare_sample_spec "$SAMPLES_DIR/counting.yaml" "$NAMESPACE" "$(unix_nano)")

    run_sample_test "$spec" "$NAMESPACE" "0" "${SKIP_RESTORE:-0}"
}

# bats test_tags=samples,cpu
@test "Sample: counting-multicontainer.yaml - Multi-container Pod" {
    if [ -z "$SAMPLES_DIR" ]; then
        skip "SAMPLES_DIR not set"
    fi
    if [ ! -f "$SAMPLES_DIR/counting-multicontainer.yaml" ]; then
        skip "counting-multicontainer.yaml not found"
    fi
    if ! should_test_sample "counting-multicontainer.yaml"; then
        skip "Filtered out by TEST_FILTER"
    fi

    local spec
    spec=$(prepare_sample_spec "$SAMPLES_DIR/counting-multicontainer.yaml" "$NAMESPACE" "$(unix_nano)")

    run_sample_test "$spec" "$NAMESPACE" "0" "${SKIP_RESTORE:-0}"
}

################################################################################
# Sample-Based Tests (GPU)
################################################################################

# bats test_tags=samples,gpu
@test "Sample: cuda-vector-add.yaml - CUDA Vector Addition" {
    if [ "${GPU_ENABLED:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU_ENABLED=1)"
    fi
    if [ -z "$SAMPLES_DIR" ]; then
        skip "SAMPLES_DIR not set"
    fi
    if [ ! -f "$SAMPLES_DIR/cuda-vector-add.yaml" ]; then
        skip "cuda-vector-add.yaml not found"
    fi
    if ! should_test_sample "cuda-vector-add.yaml"; then
        skip "Filtered out by TEST_FILTER"
    fi

    local spec
    spec=$(prepare_sample_spec "$SAMPLES_DIR/cuda-vector-add.yaml" "$NAMESPACE" "$(unix_nano)")

    run_sample_test "$spec" "$NAMESPACE" "1" "${SKIP_RESTORE:-0}"
}

# bats test_tags=samples,gpu
@test "Sample: cuda-vector-add-multicontainer.yaml - CUDA Multi-container" {
    if [ "${GPU_ENABLED:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU_ENABLED=1)"
    fi
    if [ -z "$SAMPLES_DIR" ]; then
        skip "SAMPLES_DIR not set"
    fi
    if [ ! -f "$SAMPLES_DIR/cuda-vector-add-multicontainer.yaml" ]; then
        skip "cuda-vector-add-multicontainer.yaml not found"
    fi
    if ! should_test_sample "cuda-vector-add-multicontainer.yaml"; then
        skip "Filtered out by TEST_FILTER"
    fi

    local required_gpus
    required_gpus=$(get_required_gpus "$SAMPLES_DIR/cuda-vector-add-multicontainer.yaml")
    local available_gpus
    available_gpus=$(get_available_gpus)
    if [ "$available_gpus" -lt "$required_gpus" ]; then
        skip "Insufficient GPUs: need $required_gpus, have $available_gpus"
    fi

    local spec
    spec=$(prepare_sample_spec "$SAMPLES_DIR/cuda-vector-add-multicontainer.yaml" "$NAMESPACE" "$(unix_nano)")

    run_sample_test "$spec" "$NAMESPACE" "1" "${SKIP_RESTORE:-0}"
}

# bats test_tags=samples,gpu
@test "Sample: cuda-mem-throughput.yaml - CUDA Memory Throughput" {
    if [ "${GPU_ENABLED:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU_ENABLED=1)"
    fi
    if [ -z "$SAMPLES_DIR" ]; then
        skip "SAMPLES_DIR not set"
    fi
    if [ ! -f "$SAMPLES_DIR/cuda-mem-throughput.yaml" ]; then
        skip "cuda-mem-throughput.yaml not found"
    fi
    if ! should_test_sample "cuda-mem-throughput.yaml"; then
        skip "Filtered out by TEST_FILTER"
    fi

    local spec
    spec=$(prepare_sample_spec "$SAMPLES_DIR/cuda-mem-throughput.yaml" "$NAMESPACE" "$(unix_nano)")

    run_sample_test "$spec" "$NAMESPACE" "1" "${SKIP_RESTORE:-0}"
}

# bats test_tags=samples,gpu
@test "Sample: cuda-gpu-train-simple.yaml - Simple PyTorch Training" {
    if [ "${GPU_ENABLED:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU_ENABLED=1)"
    fi
    if [ -z "$SAMPLES_DIR" ]; then
        skip "SAMPLES_DIR not set"
    fi
    if [ ! -f "$SAMPLES_DIR/cuda-gpu-train-simple.yaml" ]; then
        skip "cuda-gpu-train-simple.yaml not found"
    fi
    if ! should_test_sample "cuda-gpu-train-simple.yaml"; then
        skip "Filtered out by TEST_FILTER"
    fi

    local spec
    spec=$(prepare_sample_spec "$SAMPLES_DIR/cuda-gpu-train-simple.yaml" "$NAMESPACE" "$(unix_nano)")

    run_sample_test "$spec" "$NAMESPACE" "1" "${SKIP_RESTORE:-0}"
}

# bats test_tags=samples,gpu
@test "Sample: cuda-tensorflow.yaml - TensorFlow Training" {
    if [ "${GPU_ENABLED:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU_ENABLED=1)"
    fi
    if [ -z "$SAMPLES_DIR" ]; then
        skip "SAMPLES_DIR not set"
    fi
    if [ ! -f "$SAMPLES_DIR/cuda-tensorflow.yaml" ]; then
        skip "cuda-tensorflow.yaml not found"
    fi
    if ! should_test_sample "cuda-tensorflow.yaml"; then
        skip "Filtered out by TEST_FILTER"
    fi

    local spec
    spec=$(prepare_sample_spec "$SAMPLES_DIR/cuda-tensorflow.yaml" "$NAMESPACE" "$(unix_nano)")

    run_sample_test "$spec" "$NAMESPACE" "1" "${SKIP_RESTORE:-0}"
}

# bats test_tags=samples,gpu
@test "Sample: cuda-deepspeed-train.yaml - DeepSpeed Training" {
    if [ "${GPU_ENABLED:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU_ENABLED=1)"
    fi
    if [ -z "$SAMPLES_DIR" ]; then
        skip "SAMPLES_DIR not set"
    fi
    if [ ! -f "$SAMPLES_DIR/cuda-deepspeed-train.yaml" ]; then
        skip "cuda-deepspeed-train.yaml not found"
    fi
    if ! should_test_sample "cuda-deepspeed-train.yaml"; then
        skip "Filtered out by TEST_FILTER"
    fi

    local spec
    spec=$(prepare_sample_spec "$SAMPLES_DIR/cuda-deepspeed-train.yaml" "$NAMESPACE" "$(unix_nano)")

    run_sample_test "$spec" "$NAMESPACE" "1" "${SKIP_RESTORE:-0}"
}

################################################################################
# Large GPU Tests (LLM inference, CompBio)
################################################################################

# bats test_tags=large,gpu
@test "Sample: cuda-vllm-llama-8b.yaml - vLLM Llama 8B" {
    if [ "${GPU_ENABLED:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU_ENABLED=1)"
    fi
    if [ "${LARGE_TESTS:-0}" != "1" ]; then
        skip "Large tests disabled (set LARGE_TESTS=1)"
    fi
    if [ -z "$SAMPLES_DIR" ]; then
        skip "SAMPLES_DIR not set"
    fi
    if [ ! -f "$SAMPLES_DIR/cuda-vllm-llama-8b.yaml" ]; then
        skip "cuda-vllm-llama-8b.yaml not found"
    fi
    if ! should_test_sample "cuda-vllm-llama-8b.yaml"; then
        skip "Filtered out by TEST_FILTER"
    fi

    local spec
    spec=$(prepare_sample_spec "$SAMPLES_DIR/cuda-vllm-llama-8b.yaml" "$NAMESPACE" "$(unix_nano)")

    run_sample_test "$spec" "$NAMESPACE" "1" "${SKIP_RESTORE:-0}"
}

# bats test_tags=large,gpu,compbio
@test "Sample: gromacs-simple-example.yaml - GROMACS MD Simulation" {
    if [ "${GPU_ENABLED:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU_ENABLED=1)"
    fi
    if [ "${LARGE_TESTS:-0}" != "1" ]; then
        skip "Large tests disabled (set LARGE_TESTS=1)"
    fi
    if [ -z "$SAMPLES_DIR" ]; then
        skip "SAMPLES_DIR not set"
    fi
    if [ ! -f "$SAMPLES_DIR/gromacs-simple-example.yaml" ]; then
        skip "gromacs-simple-example.yaml not found"
    fi
    if ! should_test_sample "gromacs-simple-example.yaml"; then
        skip "Filtered out by TEST_FILTER"
    fi

    local spec
    spec=$(prepare_sample_spec "$SAMPLES_DIR/gromacs-simple-example.yaml" "$NAMESPACE" "$(unix_nano)")

    run_sample_test "$spec" "$NAMESPACE" "1" "${SKIP_RESTORE:-0}"
}

# bats test_tags=large,gpu,compbio
@test "Sample: openmm.yaml - OpenMM MD Simulation" {
    if [ "${GPU_ENABLED:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU_ENABLED=1)"
    fi
    if [ "${LARGE_TESTS:-0}" != "1" ]; then
        skip "Large tests disabled (set LARGE_TESTS=1)"
    fi
    if [ -z "$SAMPLES_DIR" ]; then
        skip "SAMPLES_DIR not set"
    fi
    if [ ! -f "$SAMPLES_DIR/openmm.yaml" ]; then
        skip "openmm.yaml not found"
    fi
    if ! should_test_sample "openmm.yaml"; then
        skip "Filtered out by TEST_FILTER"
    fi

    local spec
    spec=$(prepare_sample_spec "$SAMPLES_DIR/openmm.yaml" "$NAMESPACE" "$(unix_nano)")

    run_sample_test "$spec" "$NAMESPACE" "1" "${SKIP_RESTORE:-0}"
}
