#!/usr/bin/env bash

###################
### GCP Helpers ###
###################

export KUBECONFIG=~/.kube/config
export GKE_CLUSTER_NAME="${GKE_CLUSTER_NAME:-cedana-ci-amd64}"

install_gcloud_cli() {
    debug_log "Installing GCP CLI..."

    if command -v gcloud &>/dev/null; then
        debug_log "GCP CLI already installed"
        return 0
    fi

    apt-get update
    apt-get install -y apt-transport-https ca-certificates gnupg
    echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | tee -a /etc/apt/sources.list.d/google-cloud-sdk.list
    curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key --keyring /usr/share/keyrings/cloud.google.gpg add -
    apt-get update
    apt-get install -y google-cloud-cli
    apt-get install google-cloud-cli-gke-gcloud-auth-plugin

    debug_log "GCP CLI installed"
}

configure_gcp_credentials() {
    debug_log "Configuring GCP credentials..."

    if [ -z "$GCLOUD_PROJECT_ID" ] || [ -z "$GCLOUD_SERVICE_ACCOUNT_KEY" ] || [ -z "$GCLOUD_REGION" ]; then
        error_log "GCLOUD_PROJECT_ID, GCLOUD_SERVICE_ACCOUNT_KEY, or GCLOUD_REGION environment variables are not set"
        return 1
    fi

    echo "$GCLOUD_SERVICE_ACCOUNT_KEY" > /tmp/gcp_sa_key.json
    gcloud auth activate-service-account --key-file=/tmp/gcp_sa_key.json --project="$GCLOUD_PROJECT_ID"
    rm /tmp/gcp_sa_key.json

    gcloud config set project "$GCLOUD_PROJECT_ID"
    gcloud config set compute/region "$GCLOUD_REGION"

    debug_log "GCP credentials configured"
}

setup_cluster() {
    install_gcloud_cli
    configure_gcp_credentials

    debug_log "Setting up $GKE_CLUSTER_NAME GKE cluster..."

    gcloud container clusters get-credentials "$GKE_CLUSTER_NAME" --region "$GCLOUD_REGION" --project "$GCLOUD_PROJECT_ID"

    debug_log "GKE cluster $GKE_CLUSTER_NAME is ready"
}

teardown_cluster() {
    debug_log "Tearing down GKE cluster $GKE_CLUSTER_NAME..."

    # NOTE: Since we reuse the cluster, we don't do anything here.

    debug_log "GKE cluster $GKE_CLUSTER_NAME teardown complete"
}
