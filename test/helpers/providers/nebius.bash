#!/usr/bin/env bash

#######################
### Nebius Provider ###
#######################
#
# Nebius is unique in that it creates/destroys nodegroups dynamically for GPU tests.
#
# Environment variables:
#   NB_SA_ID                - Service account ID
#   NB_PUBLIC_KEY_ID        - Public key ID
#   NB_SA_PRIVATE_KEY       - Private key content
#   NB_SA_PRIVATE_KEY_PATH  - Path to store private key (default: /tmp/nb_sa_key)
#   NB_PROJECT_ID           - Nebius project ID
#   NB_CLUSTER_NAME         - Cluster name (default: cedana-ci-amd64)
#

export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
export NB_CLUSTER_NAME="${NB_CLUSTER_NAME:-cedana-ci-amd64}"
export NB_SA_PRIVATE_KEY_PATH="${NB_SA_PRIVATE_KEY_PATH:-/tmp/nb_sa_key}"
export GPU_OPERATOR_NAMESPACE="${GPU_OPERATOR_NAMESPACE:-gpu-operator}"
export GPU_OPERATOR_VERSION="${GPU_OPERATOR_VERSION:-v25.3.4}"
export GPU_OPERATOR_DRIVER_VERSION="${GPU_OPERATOR_DRIVER_VERSION:-570.195.03}"
export NB_NODEGROUP_NAME="${NB_NODEGROUP_NAME:-github-ci}"
export NB_NODE_COUNT="${NB_NODE_COUNT:-2}"
export NB_NODE_DISK_SIZE="${NB_NODE_DISK_SIZE:-1099511627776}"
export NB_GPU_PRESET="${NB_GPU_PRESET:-1gpu-16vcpu-200gb}"
export NB_GPU_PLATFORM="${NB_GPU_PLATFORM:-gpu-h100-sxm}"

# Internal state
NB_CLUSTER_ID=""
NB_SUBNET_ID=""
NB_NODEGROUP_ID=""

_install_nebius_cli() {
    debug_log "Installing Nebius CLI..."

    if command -v nebius &>/dev/null; then
        debug_log "Nebius CLI already installed"
        return 0
    fi

    curl -sSL https://storage.eu-north1.nebius.cloud/cli/install.sh | bash
    # set binary path
    export PATH="$HOME/.nebius/bin:$PATH"

    nebius version

    debug_log "Nebius CLI installed"
}

_configure_nebius_credentials() {
    debug_log "Configuring Nebius credentials..."

    export NB_SA_PROFILE_NAME="github-actions"
    printf '%s' "$NB_SA_PRIVATE_KEY" > "$NB_SA_PRIVATE_KEY_PATH"
    chmod 600 "$NB_SA_PRIVATE_KEY_PATH"

    nebius profile create \
        --endpoint api.nebius.cloud \
        --service-account-id "$NB_SA_ID" \
        --public-key-id "$NB_PUBLIC_KEY_ID" \
        --private-key-file "$NB_SA_PRIVATE_KEY_PATH" \
        --profile "$NB_SA_PROFILE_NAME" \
        --parent-id "$NB_PROJECT_ID"

    debug_log "Nebius credentials configured"
}

_create_nebius_mk8s() {
    debug_log "Creating Nebius mk8s cluster..."

    NB_SUBNET_ID=$(nebius vpc subnet list \
            --format json \
        | jq -r '.items[0].metadata.id')
    export NB_SUBNET_ID

    NB_CLUSTER_ID=$(
        nebius mk8s cluster get-by-name \
            --name "$NB_CLUSTER_NAME" \
            --format json | jq -r '.metadata.id'
    )

    if [ -n "$NB_CLUSTER_ID" ] && [ "$NB_CLUSTER_ID" != "null" ]; then
        debug_log "Cluster already exists, skip creation..."
    else
        NB_CLUSTER_ID=$(nebius mk8s cluster create \
                --name "$NB_CLUSTER_NAME" \
                --control-plane-subnet-id "$NB_SUBNET_ID" \
                '{"spec": { "control_plane": { "endpoints": {"public_endpoint": {}}}}}' \
            --format json | jq -r '.metadata.id')
    fi
    export NB_CLUSTER_ID

    debug_log "Nebius mk8s cluster ready with ID: $NB_CLUSTER_ID"
}

create_nodegroup() {

    debug_log "Creating Nebius node-group with H100..."

    local existing_nodegroup
    existing_nodegroup=$(nebius mk8s node-group list --parent-id "$NB_CLUSTER_ID" \
        --format json | jq -r ".items[]? | select(.metadata.name==\"$NB_NODEGROUP_NAME\") | .metadata.id" 2>/dev/null || echo "")

    if [ -n "$existing_nodegroup" ] && [ "$existing_nodegroup" != "null" ]; then
        debug_log "Node-group already exists, skipping creation..."
        NB_NODEGROUP_ID="$existing_nodegroup"
    else
        debug_log "Creating new node-group..."
        nebius mk8s node-group create \
            --name "$NB_NODEGROUP_NAME" \
            --parent-id "$NB_CLUSTER_ID" \
            --template-boot-disk-size-bytes "$NB_NODE_DISK_SIZE" \
            --fixed-node-count "$NB_NODE_COUNT" \
            --template-resources-platform "$NB_GPU_PLATFORM" \
            --template-resources-preset "$NB_GPU_PRESET" \
            --template-network-interfaces "[{\"public_ip_address\": {},\"subnet_id\": \"$NB_SUBNET_ID\"}]"
        debug_log "Nebius node-group with H100 has been created"
    fi
    export NB_NODEGROUP_ID
}

delete_nodegroup() {
    local nodegroup_name="${1:-github-ci-Nebius}"

    debug_log "Deleting Nebius node-group..."

    export NB_NODEGROUP_NAME="$nodegroup_name"
    NB_CLUSTER_ID=$(
        nebius mk8s cluster get-by-name \
            --name "$NB_CLUSTER_NAME" \
            --format json | jq -r '.metadata.id'
    )
    local nodegroup_id
    nodegroup_id=$(nebius mk8s node-group get-by-name \
            --parent-id "$NB_CLUSTER_ID" \
        --name "$NB_NODEGROUP_NAME" --format json | jq -r '.metadata.id')

    if [ -z "$nodegroup_id" ] || [ "$nodegroup_id" = "null" ]; then
        debug_log "Cluster Node-group does not exist, skipping deletion..."
    else
        debug_log "Deleting node-group..."
        nebius mk8s node-group delete --id "$nodegroup_id"
        debug_log "Nebius node-group has been deleted"
    fi
}

delete_mk8s_cluster() {
    local cluster_id
    cluster_id=$(nebius mk8s cluster get-by-name \
        --name "$NB_CLUSTER_NAME" --format json | jq -r '.metadata.id')

    if [ -z "$cluster_id" ] || [ "$cluster_id" = "null" ]; then
        debug_log "Cluster does not exist, skipping deletion..."
    else
        debug_log "Deleting Nebius MK8s cluster..."
        nebius mk8s cluster delete --id "$cluster_id"
        debug_log "Nebius MK8s cluster has been deleted"
    fi
}

setup_gpu_operator() {
    debug_log "Installing NVIDIA GPU operator..."

    helm repo add nvidia https://helm.ngc.nvidia.com/nvidia \
        && helm repo update

    helm upgrade -i --wait gpu-operator \
        -n "$GPU_OPERATOR_NAMESPACE" --create-namespace \
        nvidia/gpu-operator \
        --version="$GPU_OPERATOR_VERSION" --set driver.version="$GPU_OPERATOR_DRIVER_VERSION"

    wait_for_ready "$GPU_OPERATOR_NAMESPACE" 120
    wait_for_cmd 120 is_gpu_available 1

    debug_log "NVIDIA GPU operator installed successfully"
}

setup_cluster() {
    _install_nebius_cli
    _configure_nebius_credentials
    _create_nebius_mk8s
    create_nodegroup
    debug_log "Creating nebius multi_gpu nodegroup ..."
    NB_NODEGROUP_NAME="gci-multi-gpu-nebius"
    NB_NODE_COUNT="1"
    NB_GPU_PRESET="8gpu-128vcpu-1600gb"
    NB_NODE_DISK_SIZE="137438953472"
    create_nodegroup
    debug_log "Fetching Nebius mk8s kubeconfig file..."

    nebius mk8s cluster get-credentials --id "$NB_CLUSTER_ID" --external

    debug_log "Nebius mk8s kubeconfig file has been fetched"

    # Uncomment if GPU operator needs to be installed
    setup_gpu_operator
}

teardown_cluster() {
    debug_log "Tearing down Nebius cluster..."

    # Delete the H100 nodegroup
    NB_NODEGROUP_NAME="github-ci"
    NB_NODE_COUNT="2"
    NB_GPU_PRESET="1gpu-16vcpu-200gb"
    # 1TB disk size
    NB_NODE_DISK_SIZE="1099511627776"
    delete_nodegroup

    # Delete the H100 multi-gpu nodegroup
    NB_NODEGROUP_NAME="gci-multi-gpu-nebius"
    NB_NODE_COUNT="1"
    NB_GPU_PRESET="8gpu-128vcpu-1600gb"
    # 128GB disk size
    NB_NODE_DISK_SIZE="137438953472"
    delete_nodegroup

    delete_mk8s_cluster

    debug_log "Nebius cluster teardown complete"
}
