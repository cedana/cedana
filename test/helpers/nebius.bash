#!/usr/bin/env bash

######################
### Nebius Helpers ###
######################

export KUBECONFIG=~/.kube/config
export H100_CLUSTER_NAME="${H100_CLUSTER_NAME:-cedana-ci-arm64}"

install_nebius_cli() {
    debug_log "Installing Nebius CLI..."

    if command -v nebius version &>/dev/null; then
        debug_log "nebius CLI already installed"
        return 0
    fi

    local arch
    arch=$(uname -m)

    curl -sSL https://storage.eu-north1.nebius.cloud/cli/install.sh | bash
    # set binary path
    export PATH="/github/home/.nebius/bin:$PATH"

    nebius version

    debug_log "Nebius CLI installed"
}

configure_nebius_credentials() {
    debug_log "Configuring Nebius credentials..."

    export NB_SA_PROFILE_NAME="github-actions"
    printf '%s' "$NB_SA_PRIVATE_KEY" > "$NB_SA_PRIVATE_KEY_PATH"
    chmod 600 "$NB_SA_PRIVATE_KEY_PATH"

    export NB_CLUSTER_ID=$(
        nebius mk8s cluster get-by-name \
            --name "$H100_CLUSTER_NAME" \
            --format json | jq -r '.metadata.id'
    )
    if [ -n "$CLUSTER_ID" ] && [ "$CLUSTER_ID" != "null" ]; then
        debug_log "Cluster already exists, skipp creation..."
    else
    nebius profile create \
        --endpoint api.nebius.cloud \
        --service-account-id "$NB_SA_ID" \
        --public-key-id "$NB_PUBLIC_KEY_ID" \
        --private-key-file "$NB_SA_PRIVATE_KEY_PATH" \
        --profile "$NB_SA_PROFILE_NAME" \
        --parent-id "$NB_PROJECT_ID"
    fi

    debug_log "Nebius credentials configured"
}

create_nebius_mk8s() {
    debug_log "Creating Nebius mk8s with H100..."
    export NB_SUBNET_ID=$(nebius vpc subnet list \
    --format json \
    | jq -r '.items[0].metadata.id')

    export NB_CLUSTER_ID=$(nebius mk8s cluster create \
        --name "$H100_CLUSTER_NAME" \
        --control-plane-subnet-id $NB_SUBNET_ID \
        '{"spec": { "control_plane": { "endpoints": {"public_endpoint": {}}}}}' \
        --format json | jq -r '.metadata.id')

    debug_log "Nebius mk8s with H100 has been created"
}

create_nebius_nodegroup() {
    debug_log "Creating Nebius node-group with H100..."

    export NB_NODEGROUP_NAME="github-ci-H100"
    EXISTING_NODEGROUP=$(nebius mk8s node-group list --parent-id "$NB_CLUSTER_ID" \
        --format json | jq -r ".items[] | select(.metadata.name==\"$NB_NODEGROUP_NAME\") | .metadata.id")
    if [ -n "$EXISTING_NODEGROUP" ] && [ "$EXISTING_NODEGROUP" != "null" ]; then
        debug_log "Node-group already exists, skipping creation..."
        export NB_NODEGROUP_ID="$EXISTING_NODEGROUP"
    else
        debug_log "Creating new node-group..."
        nebius mk8s node-group create \
            --name "$NB_NODEGROUP_NAME" \
            --parent-id "$NB_CLUSTER_ID" \
            --template-boot-disk-size-bytes 137438953472 \
            --fixed-node-count 1 \
            --template-resources-platform "gpu-h100-sxm" \
            --template-resources-preset "1gpu-16vcpu-200gb" \
            --template-network-interfaces "[{\"public_ip_address\": {},\"subnet_id\": \"$NB_SUBNET_ID\"}]"

        debug_log "Nebius node-group with H100 has been created"
    fi
}

setup_nebius_cluster() {

    install_nebius_cli
    configure_nebius_credentials
    create_nebius_mk8s
    create_nebius_nodegroup

    debug_log "Fetching Nebius mk8s kubeconfig file..."

    nebius mk8s cluster get-credentials \--id $NB_CLUSTER_ID --external

    debug_log "Nebius mk8 kubeconfig file has been fetched"
}
