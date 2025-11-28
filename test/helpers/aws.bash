#!/usr/bin/env bash

###################
### EKS Helpers ###
###################

export KUBECONFIG=~/.kube/config
export EKS_CLUSTER_NAME="${EKS_CLUSTER_NAME:-cedana-ci-arm64}"
export H100_CLUSTER_NAME="${H100_CLUSTER_NAME:-cedana-ci-arm64}"

install_aws_cli() {
    debug_log "Installing AWS CLI..."

    if command -v aws &>/dev/null; then
        debug_log "AWS CLI already installed"
        return 0
    fi

    local arch
    arch=$(uname -m)

    curl "https://awscli.amazonaws.com/awscli-exe-linux-${arch}.zip" -o "/tmp/awscli.zip"
    unzip -q /tmp/awscli.zip -d /tmp
    /tmp/aws/install --update
    rm -rf /tmp/awscli.zip /tmp/aws
    debug_log "AWS CLI installed"
}

configure_aws_credentials() {
    debug_log "Configuring AWS credentials..."

    if [ -z "$AWS_ACCESS_KEY_ID" ] || [ -z "$AWS_SECRET_ACCESS_KEY" ] || [ -z "$AWS_REGION" ]; then
        error_log "AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY or AWS_REGION are not set"
        return 1
    fi

    aws configure set aws_access_key_id "$AWS_ACCESS_KEY_ID"
    aws configure set aws_secret_access_key "$AWS_SECRET_ACCESS_KEY"
    aws configure set default.region "$AWS_REGION"

    debug_log "AWS credentials configured"
}

setup_cluster() {
    install_aws_cli
    configure_aws_credentials

    debug_log "Setting up $EKS_CLUSTER_NAME EKS cluster..."

    aws eks update-kubeconfig --region "$AWS_REGION" --name "$EKS_CLUSTER_NAME" --kubeconfig "$KUBECONFIG"

    debug_log "EKS cluster $EKS_CLUSTER_NAME is ready"
}

teardown_cluster() {
    debug_log "Tearing down EKS cluster $EKS_CLUSTER_NAME..."

    # NOTE: Since we reuse the cluster, we don't do anything here.

    debug_log "EKS cluster $EKS_CLUSTER_NAME teardown complete"
}

install_nebius_cli() {
    debug_log "Installing Nebius CLI..."

    if command -v nebius version &>/dev/null; then
        debug_log "nebius CLI already installed"
        return 0
    fi

    local arch
    arch=$(uname -m)

    curl -sSL https://storage.eu-north1.nebius.cloud/cli/install.sh | bash
    exec -l $SHELL
    debug_log "Nebius CLI installed"
}

configure_nebius_credentials() {
    debug_log "Configuring Nebius credentials..."

    if [ -z "$AWS_ACCESS_KEY_ID" ] || [ -z "$AWS_SECRET_ACCESS_KEY" ] || [ -z "$AWS_REGION" ]; then
        error_log "AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY or AWS_REGION are not set"
        return 1
    fi

    debug_log "Nebius credentials configured"
}

create_nebius_mk8s() {
    debug_log "Creating Nebius mk8s with H100..."
        nebius mk8s cluster create \
        --name "$H100" \
        --region "$region" \
        --wait=false \
        || return 1
    debug_log "Nebius mk8s with H100 has been created"
}
#TODO nebius create nodegroup helper
    nebius mk8s node-group create \
        --name "$NEBIUS_NODEGROUP_NAME" \
        --template-resources-platform "$PLATFORM" \
        --template-resources-preset "$PRESET_1" \
        --profile "$NEBIUS_PROFILE" \

setup_nebius_kubeconfig() {
    debug_log "Fetching Nebius mk8s kubeconfig file..."
    nebius mk8s cluster get-credentials \--id $NB_CLUSTER_ID --external
    debug_log "Nebius mk8 kubeconfig file has been fetched"
}
