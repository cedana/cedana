#!/usr/bin/env bash

install_helm() {
    debug_log "Installing helm..."
    curl -fsSL -o /tmp/get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
    chmod 700 /tmp/get_helm.sh
    /tmp/get_helm.sh
}

helm_install_cedana() {
    install_helm

    debug_log "Installing helm chart... (chart: $HELM_CHART)"

    local cluster_name="$1"
    local namespace="$2"

    local helm_cmd

    if [ -e "$HELM_CHART" ]; then
        helm_cmd="helm install cedana $HELM_CHART" # local path to chart
    elif [ -n "$HELM_CHART" ]; then
        helm_cmd="helm install cedana oci://registry-1.docker.io/cedana/cedana-helm --version $HELM_CHART"
    else
        helm_cmd="helm install cedana oci://registry-1.docker.io/cedana/cedana-helm" # latest
    fi
    helm_cmd="$helm_cmd --create-namespace -n $namespace"
    helm_cmd="$helm_cmd --set cedanaConfig.cedanaUrl=$CEDANA_URL"
    helm_cmd="$helm_cmd --set cedanaConfig.cedanaAuthToken=$CEDANA_AUTH_TOKEN"
    helm_cmd="$helm_cmd --set cedanaConfig.cedanaClusterName=$cluster_name"
    helm_cmd="$helm_cmd --set cedanaConfig.logLevel=$CEDANA_LOG_LEVEL"
    helm_cmd="$helm_cmd --set cedanaConfig.checkpointStreams=$CEDANA_CHECKPOINT_STREAMS"
    helm_cmd="$helm_cmd --set cedanaConfig.gpuShmSize=$CEDANA_GPU_SHM_SIZE"
    helm_cmd="$helm_cmd --set cedanaConfig.pluginsBuilds=local" # don't download any from registry
    if [ -n "$CONTROLLER_REPO" ]; then
        helm_cmd="$helm_cmd --set controllerManager.manager.image.repository=$CONTROLLER_REPO"
        if [ -n "$CONTROLLER_TAG" ]; then
            helm_cmd="$helm_cmd --set controllerManager.manager.image.tag=$CONTROLLER_TAG"
        fi
        if [ -n "$CONTROLLER_DIGEST" ]; then
            helm_cmd="$helm_cmd --set controllerManager.manager.image.digest=$CONTROLLER_DIGEST"
        fi
        helm_cmd="$helm_cmd --set controllerManager.manager.image.pullPolicy=Always"
    fi
    if [ -n "$HELPER_REPO" ]; then
        helm_cmd="$helm_cmd --set daemonHelper.image.repository=$HELPER_REPO"
        if [ -n "$HELPER_TAG" ]; then
            helm_cmd="$helm_cmd --set daemonHelper.image.tag=$HELPER_TAG"
        fi
        if [ -n "$HELPER_DIGEST" ]; then
            helm_cmd="$helm_cmd --set daemonHelper.image.digest=$HELPER_DIGEST"
        fi
        helm_cmd="$helm_cmd --set daemonHelper.image.pullPolicy=Always"
    fi

    $helm_cmd || {
        error_log "Error: Failed to install helm chart"
        error kubectl logs -n "$namespace" -l app.kubernetes.io/instance=cedana --tail=1000 --prefix=true
        return 1
    }
}

helm_uninstall_cedana() {
    local namespace="$1"

    debug_log "Uninstalling helm chart..."
    helm uninstall cedana -n "$namespace" --wait --timeout=2m || {
        error_log "Error: Failed to uninstall helm chart"
        return 1
    }

    debug_log "Waiting for all pods in $namespace namespace to terminate..."

    wait_for_cmd 60 "kubectl get pods -n $namespace --no-headers 2>/dev/null | grep -q ."

    debug_log "Helm chart uninstalled successfully"
}
