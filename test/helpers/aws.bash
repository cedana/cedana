#!/usr/bin/env bash

###################
### EKS Helpers ###
###################

export KUBECONFIG=~/.kube/config
export EKS_CLUSTER_NAME="${EKS_CLUSTER_NAME:-cedana-ci-amd64}"

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
