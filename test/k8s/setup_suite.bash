#!/bin/bash

# This file contains setup functions that run for the duration of the test suite run.

################################################################################
# Unified Kubernetes Tests
################################################################################
#
# This test file runs against any Kubernetes cluster using the provider abstraction.
# Select provider via PROVIDER environment variable.
#
# Providers:
#   generic  - Pre-configured cluster (default) - requires kubectl to be configured
#   aws/eks  - Amazon EKS - requires AWS credentials
#   gcp/gke  - Google GKE - requires GCP credentials
#   nebius   - Nebius Cloud - requires Nebius credentials, creates GPU nodegroups
#   k3s      - Local K3s - creates fresh cluster
#
# Environment variables:
#   PROVIDER                        - Provider to use (default: generic)
#   KUBECONFIG                      - Path to kubeconfig for generic provider (default: ~/.kube/config)
#   CLUSTER_NAME                    - Name for registering cluster with propagator (default: auto-generated)
#   CLUSTER_ID                      - If set, skip cluster registration and use this ID
#   SKIP_HELM                       - If set to "1", skip helm install/uninstall (assumes Cedana is already installed)
#   GPU                             - If set to "1", run GPU tests
#   CEDANA_NAMESPACE                - Namespace for Cedana components (default: cedana-system)
#   NAMESPACE                       - Namespace for test pods (default: test)
#   SAMPLES_DIR                     - Path to cedana-samples/kubernetes (default: auto-detect)

################################################################################
# Setup and Configuration
################################################################################

# Defaults for remote checkpoint storage
export CEDANA_CHECKPOINT_DIR=${CEDANA_CHECKPOINT_DIR:-cedana://ci}
export CEDANA_CHECKPOINT_COMPRESSION=${CEDANA_CHECKPOINT_COMPRESSION:-lz4}

source "${BATS_TEST_DIRNAME}"/../helpers/utils.bash
source "${BATS_TEST_DIRNAME}"/../helpers/containerd.bash
source "${BATS_TEST_DIRNAME}"/../helpers/daemon.bash
source "${BATS_TEST_DIRNAME}"/../helpers/providers/provider.bash
source "${BATS_TEST_DIRNAME}"/../helpers/k8s.bash
source "${BATS_TEST_DIRNAME}"/../helpers/helm.bash
source "${BATS_TEST_DIRNAME}"/../helpers/propagator.bash
source "${BATS_TEST_DIRNAME}"/../helpers/metrics.bash

# Generate cluster name if not provided
if [ -z "$CLUSTER_NAME" ]; then
    CLUSTER_NAME="test-${PROVIDER}-$(unix_nano)"
fi
if [ "$PROVIDER" == "nebius" ]; then
    export CEDANA_GPU_SHM_SIZE="${CEDANA_GPU_SHM_SIZE:-$((8*GIBIBYTE))}"
    export CEDANA_CHECKPOINT_DIR=${CEDANA_CHECKPOINT_DIR:-/tmp}
fi
export CLUSTER_NAME
export CLUSTER_ID
export NAMESPACE="${NAMESPACE:-test}"
export CEDANA_NAMESPACE="${CEDANA_NAMESPACE:-cedana-system}"
export TAIL_PID=""

setup_suite() {
    install_kubectl
    install_helm
    install_k9s

    setup_samples
    setup_cluster

    # Verify kubectl connectivity
    if ! kubectl cluster-info &>/dev/null; then
        error_log "Cannot connect to Kubernetes cluster after provider setup."
        return 1
    fi

    debug_log "Connected to $PROVIDER cluster: $(kubectl config current-context)"

    # Start tailing logs in background
    tail_all_logs "$CEDANA_NAMESPACE" 600 &
    TAIL_PID=$!

    # Install Cedana helm chart unless skipped
    if [ "${SKIP_HELM:-0}" != "1" ]; then
        if [ -z "$CLUSTER_ID" ]; then
            debug_log "Registering cluster '$CLUSTER_NAME' with propagator..."
            CLUSTER_ID=$(register_cluster "$CLUSTER_NAME")
            export CLUSTER_ID
            info_log "======================================="
            info_log "Cluster registered with ID: $CLUSTER_ID"
            info_log "Logs for this cluster can be viewed at:"
            info log_url_cluster "$CEDANA_URL" "$CLUSTER_ID"
            info_log "======================================="
        else
            debug_log "Using provided cluster ID: $CLUSTER_ID"
        fi

        helm_uninstall_cedana "$CEDANA_NAMESPACE"
        helm_install_cedana "$CLUSTER_ID" "$CEDANA_NAMESPACE"
    else
        debug_log "Skipping helm install (SKIP_HELM=1)"
        if [ -z "$CLUSTER_ID" ]; then
            CLUSTER_ID=$(kubectl get cm cedana-config -n "$CEDANA_NAMESPACE" -o jsonpath='{.data.cluster-id}' 2>/dev/null)
            if [ -z "$CLUSTER_ID" ]; then
                error_log "SKIP_HELM=1 but no cedana-config configmap found. Please provide CLUSTER_ID."
                return 1
            fi
            export CLUSTER_ID
            debug_log "Using cluster ID from existing installation: $CLUSTER_ID"
        fi
    fi

    wait_for_ready "$CEDANA_NAMESPACE" 600

    # Restart log tailing (as it can be broken on kubelet restart)
    if [ -n "$TAIL_PID" ]; then
        pkill -P "$TAIL_PID" 2>/dev/null || true
        tail_all_logs "$CEDANA_NAMESPACE" 120 &
        TAIL_PID=$!
    fi

    # Create test namespace
    create_namespace "$NAMESPACE"
}

teardown_suite() {
    # Stop log tailing
    if [ -n "$TAIL_PID" ]; then
        pkill -P "$TAIL_PID" 2>/dev/null || true
    fi

    # Clean up test namespace
    delete_namespace "$NAMESPACE" --force

    # Clean up any leftover PVs from tests
    kubectl delete pv --all --wait=false 2>/dev/null || true

    # Uninstall helm chart unless skipped
    if [ "${SKIP_HELM:-0}" != "1" ]; then
        helm_uninstall_cedana "$CEDANA_NAMESPACE"
    else
        debug_log "Skipping helm uninstall"
    fi

    # Deregister cluster (only if we registered it)
    if [ -n "$CLUSTER_ID" ] && [ "${SKIP_HELM:-0}" != "1" ]; then
        deregister_cluster "$CLUSTER_ID"
    else
        debug_log "Skipping cluster deregistration"
    fi

    # Teardown cluster using provider
    teardown_cluster
}
