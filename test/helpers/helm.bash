#!/usr/bin/env bash

install_helm() {
    debug_log "Installing helm..."
    curl -fsSL -o /tmp/get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
    chmod 700 /tmp/get_helm.sh
    /tmp/get_helm.sh
}

helm_install_cedana() {
    install_helm

    debug_log "Installing Cedana helm chart..."

    local helm_cmd="helm install cedana oci://registry-1.docker.io/cedana/cedana-helm"
    helm_cmd="$helm_cmd --create-namespace -n cedana-system"
    helm_cmd="$helm_cmd --set cedanaConfig.cedanaUrl=$CEDANA_URL"
    helm_cmd="$helm_cmd --set cedanaConfig.cedanaAuthToken=$CEDANA_AUTH_TOKEN"
    helm_cmd="$helm_cmd --set cedanaConfig.cedanaClusterName=test-cluster"
    helm_cmd="$helm_cmd --set cedanaConfig.logLevel=$CEDANA_LOG_LEVEL"
    helm_cmd="$helm_cmd --set cedanaConfig.checkpointStreams=$CEDANA_CHECKPOINT_STREAMS"
    helm_cmd="$helm_cmd --set cedanaConfig.gpuShmSize=$CEDANA_GPU_SHM_SIZE"
    helm_cmd="$helm_cmd --set cedanaConfig.pluginsBuilds=release" # don't download any from registry
    if [ -n "$CEDANA_CONTROLLER_TAG" ]; then
        helm_cmd="$helm_cmd --set controllerManager.manager.image.repository=$CEDANA_CONTROLLER_REPO"
        helm_cmd="$helm_cmd --set controllerManager.manager.image.tag=$CEDANA_CONTROLLER_TAG"
        helm_cmd="$helm_cmd --set controllerManager.manager.image.pullPolicy=Always"
    fi

    helm_cmd="$helm_cmd --set daemonHelper.image.repository=cedana/cedana-helper-test"
    helm_cmd="$helm_cmd --set daemonHelper.image.tag=feat-ced-1201"
    helm_cmd="$helm_cmd --set daemonHelper.image.pullPolicy=Always"

    helm_cmd="$helm_cmd --wait --timeout=10m"
    debug $helm_cmd
    if [ $? -ne 0 ]; then
        debug_log "Error: Failed to install Cedana helm chart"
        debug kubectl get pods -n cedana-system || true
        debug kubectl logs -n cedana-system --all-containers=true --prefix=true || true
        return 1
    fi

    debug_log "Waiting for Cedana components to become ready..."

    kubectl wait --for=condition=Ready pod \
        -l app.kubernetes.io/instance=cedana \
        -n cedana-system \
        --timeout=300s
    if [ $? -ne 0 ]; then
        debug_log "Error: Cedana components failed to become ready"
        debug kubectl get pods -n cedana-system || true
        debug kubectl describe pods -n cedana-system || true
        debug kubectl logs -n cedana-system --all-containers=true --prefix=true || true
        return 1
    fi

    debug kubectl logs -f -n cedana-system -l app.kubernetes.io/instance=cedana --tail=1000 --prefix=true &
}

helm_uninstall_cedana() {
    debug_log "Uninstalling Cedana helm chart..."
    helm uninstall cedana -n cedana-system

    if [ $? -ne 0 ]; then
        debug_log "Error: Failed to uninstall Cedana helm chart"
        return 1
    fi

    debug_log "Waiting for all pods in cedana-system namespace to terminate..."
    while kubectl get pods -n cedana-system --no-headers 2>/dev/null | grep -q .; do
        sleep 2
    done

    kubectl delete namespace cedana-system --ignore-not-found=true
}
