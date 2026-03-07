#!/usr/bin/env bash

############################
### Provider Interface   ###
############################
#
# This file auto-loads the correct provider based on PROVIDER env var.
# All providers must implement:
#   - setup_cluster()    : Configure kubectl access to the cluster
#   - teardown_cluster() : Clean up cluster resources (optional for persistent clusters)
#
# Optional provider functions:
#   - create_nodegroup() : Create GPU/specialized nodegroups (e.g., Nebius H100)
#   - delete_nodegroup() : Delete nodegroups on teardown
#   - setup_gpu_operator() : Install NVIDIA GPU operator if needed
#
# Environment variables:
#   PROVIDER - Provider to use: aws|eks|gcp|gke|nebius|k3s|generic (default: generic)
#

PROVIDER="${PROVIDER:-generic}"

# Normalize provider names
case "$PROVIDER" in
    aws|eks|EKS)
        PROVIDER="aws"
        ;;
    eks-karpenter|EKS-Karpenter|EKS_KARPENTER)
        PROVIDER="eks-karpenter"
        ;;
    gcp|gke|GKE)
        PROVIDER="gcp"
        ;;
    nebius|Nebius|NEBIUS)
        PROVIDER="nebius"
        ;;
    k3s|K3s|K3S)
        PROVIDER="k3s"
        ;;
    generic|GENERIC|*)
        PROVIDER="generic"
        ;;
esac

export PROVIDER

# Get the directory where this script is located
PROVIDERS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source the appropriate provider
case "$PROVIDER" in
    aws)
        source "${PROVIDERS_DIR}/aws.bash"
        ;;
    eks-karpenter)
        source "${PROVIDERS_DIR}/eks-karpenter.bash"
        ;;
    gcp)
        source "${PROVIDERS_DIR}/gcp.bash"
        ;;
    nebius)
        source "${PROVIDERS_DIR}/nebius.bash"
        ;;
    k3s)
        source "${PROVIDERS_DIR}/k3s.bash"
        ;;
    generic)
        source "${PROVIDERS_DIR}/generic.bash"
        ;;
esac

# Verify provider implements required functions
_verify_provider_interface() {
    local required_functions=("setup_cluster" "teardown_cluster")

    for fn in "${required_functions[@]}"; do
        if ! declare -f "$fn" > /dev/null 2>&1; then
            error_log "Provider '$PROVIDER' must implement $fn()"
            return 1
        fi
    done

    debug_log "Provider '$PROVIDER' loaded successfully"
    return 0
}

# Call verification (will be executed when this file is sourced)
_verify_provider_interface || exit 1
