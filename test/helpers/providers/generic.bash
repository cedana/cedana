#!/usr/bin/env bash

#######################
### Generic Provider ###
#######################
#
# Generic provider for pre-configured Kubernetes clusters.
# Assumes kubectl is already configured and can reach the cluster.
# Does NOT create or destroy clusters - just validates connectivity.
#
# Use this provider when:
#   - Running against an existing cluster (EKS, GKE, etc.)
#   - Testing with a pre-configured kubeconfig
#   - Running in CI with a persistent cluster
#
# Environment variables:
#   KUBECONFIG - Path to kubeconfig (default: ~/.kube/config)
#

export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"

setup_cluster() {
    debug_log "Using pre-configured cluster (generic provider)..."

    # Validate kubectl connectivity
    if ! kubectl cluster-info &>/dev/null; then
        error_log "Cannot connect to Kubernetes cluster."
        error_log "Ensure KUBECONFIG is set correctly or kubectl is configured."
        error_log "KUBECONFIG=$KUBECONFIG"
        return 1
    fi

    local context
    context=$(kubectl config current-context 2>/dev/null || echo "unknown")
    debug_log "Connected to cluster context: $context"

    # Show cluster info for debugging
    debug_log "Cluster nodes:"
    debug kubectl get nodes -o wide 2>/dev/null || true

    debug_log "Generic provider setup complete"
}

teardown_cluster() {
    debug_log "Generic provider: no cluster teardown (cluster is persistent)"
    # No-op - don't destroy pre-configured clusters
}

# Optional interface functions (no-ops for generic provider)
create_nodegroup() {
    debug_log "Generic provider: nodegroup management not supported"
    debug_log "Manage nodegroups through your cloud provider's console/CLI"
}

delete_nodegroup() {
    debug_log "Generic provider: nodegroup management not supported"
}

setup_gpu_operator() {
    debug_log "Generic provider: GPU operator installation not implemented"
    debug_log "Install NVIDIA GPU operator manually if needed"
}
