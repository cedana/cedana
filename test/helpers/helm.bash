#!/usr/bin/env bash

install_helm() {
    if command -v helm &>/dev/null; then
        debug_log "helm is already installed"
        return 0
    fi
    debug_log "Installing helm..."
    curl -fsSL -o /tmp/get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
    chmod 700 /tmp/get_helm.sh
    /tmp/get_helm.sh
    rm -f /tmp/get_helm.sh
    debug_log "helm installed"
}

# Installs (or upgrades to) the chart under test, with this build's images and plugins. With a
# released chart version ("latest" for the latest), installs that chart as it was released
# instead: only this deployment's values, with its own images and plugins (e.g. the previous
# release, to upgrade from).
helm_install_cedana() {
    local cluster_id="$1"
    local namespace="$2"
    local released="${3:-}"

    local chart="$HELM_CHART"
    if [ -n "$released" ]; then
        chart=""
        if [ "$released" != "latest" ]; then
            chart="$released"
        fi
    fi

    debug_log "Installing helm chart... (chart: ${chart:-latest}, namespace: $namespace, cluster_id: $cluster_id)"
    local helm_cmd

    # Use upgrade --install for idempotent installs
    if [ -z "$released" ] && [ -e "$chart" ]; then
        debug_log "Using local helm chart at $chart"
        helm_cmd="helm upgrade --install cedana $chart" # local path to chart
        if [ -f "$chart/.values.yaml" ]; then
            debug_log "Using additional values from $chart/.values.yaml"
            helm_cmd="$helm_cmd -f $chart/.values.yaml"
        fi
    elif [ -n "$chart" ]; then
        debug_log "Using helm chart version $chart from OCI registry"
        helm_cmd="helm upgrade --install cedana oci://registry-1.docker.io/cedana/cedana-helm --version $chart"
    else
        debug_log "Using latest helm chart from OCI registry"
        helm_cmd="helm upgrade --install cedana oci://registry-1.docker.io/cedana/cedana-helm" # latest
    fi

    # This deployment's values
    helm_cmd="$helm_cmd --create-namespace -n $namespace"
    helm_cmd="$helm_cmd --set config.url=$CEDANA_URL"
    helm_cmd="$helm_cmd --set config.authToken=$CEDANA_AUTH_TOKEN"
    helm_cmd="$helm_cmd --set config.clusterId=$cluster_id"
    helm_cmd="$helm_cmd --set config.logLevel=$CEDANA_LOG_LEVEL"
    helm_cmd="$helm_cmd --set config.checkpointDir=$CEDANA_CHECKPOINT_DIR"
    helm_cmd="$helm_cmd --set config.checkpointStreams=$CEDANA_CHECKPOINT_STREAMS"
    helm_cmd="$helm_cmd --set config.checkpointCompression=$CEDANA_CHECKPOINT_COMPRESSION"
    helm_cmd="$helm_cmd --set config.awsAccessKeyId=$AWS_ACCESS_KEY_ID"
    helm_cmd="$helm_cmd --set config.awsSecretAccessKey=$AWS_SECRET_ACCESS_KEY"
    helm_cmd="$helm_cmd --set config.awsRegion=$AWS_REGION"
    helm_cmd="$helm_cmd --set hostConfig.containerdAddress=$CONTAINERD_ADDRESS"
    helm_cmd="$helm_cmd --set config.profiling=$CEDANA_PROFILING_ENABLED"
    helm_cmd="$helm_cmd --set config.metrics=$CEDANA_METRICS_ENABLED"
    if [ "$GPU" != "1" ]; then
        helm_cmd="$helm_cmd --set config.pluginsGpuVersion=none"
    fi
    if [ -n "$CEDANA_GPU_SHM_SIZE" ]; then
        helm_cmd="$helm_cmd --set config.gpuShmSize=$CEDANA_GPU_SHM_SIZE"
    fi

    # This build's images and plugins, from environment variables. Not for a released chart,
    # which installs its own.
    if [ -z "$released" ]; then
        if [ -n "$CONTROLLER_REPO" ]; then
            helm_cmd="$helm_cmd --set controllerManager.manager.image.repository=$CONTROLLER_REPO"
        fi
        if [ -n "$CONTROLLER_DIGEST" ]; then
            helm_cmd="$helm_cmd --set controllerManager.manager.image.digest=$CONTROLLER_DIGEST"
        elif [ -n "$CONTROLLER_TAG" ]; then
            helm_cmd="$helm_cmd --set controllerManager.manager.image.tag=$CONTROLLER_TAG"
        fi
        if [ -n "$HELPER_REPO" ]; then
            helm_cmd="$helm_cmd --set daemonHelper.image.repository=$HELPER_REPO"
        fi
        if [ -n "$HELPER_DIGEST" ]; then
            helm_cmd="$helm_cmd --set daemonHelper.image.digest=$HELPER_DIGEST"
        elif [ -n "$HELPER_TAG" ]; then
            helm_cmd="$helm_cmd --set daemonHelper.image.tag=$HELPER_TAG"
        fi

        if [ -n "$CEDANA_PLUGINS_BUILDS" ]; then
            helm_cmd="$helm_cmd --set config.pluginsBuilds=$CEDANA_PLUGINS_BUILDS"
        fi
        if [ -n "$CEDANA_PLUGINS_NATIVE_VERSION" ]; then
            helm_cmd="$helm_cmd --set config.pluginsNativeVersion=$CEDANA_PLUGINS_NATIVE_VERSION"
        fi
        if [ -n "$CEDANA_PLUGINS_CRIU_VERSION" ]; then
            helm_cmd="$helm_cmd --set config.pluginsCriuVersion=$CEDANA_PLUGINS_CRIU_VERSION"
        fi
        if [ -n "$CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION" ]; then
            helm_cmd="$helm_cmd --set config.pluginsContainerdRuntimeVersion=$CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION"
        fi
        if [ "$GPU" == "1" ] && [ -n "$CEDANA_PLUGINS_GPU_VERSION" ]; then
            helm_cmd="$helm_cmd --set config.pluginsGpuVersion=$CEDANA_PLUGINS_GPU_VERSION"
        fi
        if [ -n "$CEDANA_PLUGINS_STREAMER_VERSION" ]; then
            helm_cmd="$helm_cmd --set config.pluginsStreamerVersion=$CEDANA_PLUGINS_STREAMER_VERSION"
        fi
    fi

    helm_cmd="$helm_cmd --wait --timeout=7m"

    debug "$helm_cmd" || {
        error_log "Failed to install helm chart"
        error kubectl logs -n "$namespace" -l app.kubernetes.io/instance=cedana --tail=1000 --prefix=true
        return 1
    }
    debug_log "Helm chart installed"
}

# Installs the plugins of the build under test, from the files in the working directory, as
# test_k8s.yml does on K3s (CEDANA_PLUGINS_BUILDS=local): e.g. again after a released chart's
# helper installed its own
install_local_plugins() {
    debug_log "Installing this build's plugins..."
    sudo -E cedana plugin install runc crun containerd containerd/runtime-runc criu \
        storage/cedana storage/s3 storage/gcs || return 1
    if [ "${GPU:-0}" = "1" ]; then
        sudo -E cedana plugin install gpu || return 1
    fi
    if [ "${STREAMER:-false}" = "true" ]; then
        sudo -E cedana plugin install streamer || return 1
    fi
}

# Upgrades the installed (released) chart to the one under test, the way customers do, with a
# checkpoint taken before it, for upgrade.bats to restore: its pod, action and last count are
# exported as UPGRADE_POD, UPGRADE_ACTION_ID and UPGRADE_COUNT, and the chart it was taken on
# as UPGRADE_FROM
helm_upgrade_cedana() {
    local cluster_id="$1"
    local namespace="$2"

    UPGRADE_FROM=$(helm list -n "$namespace" -o json | jq -r '.[] | select(.name == "cedana") | .chart')
    debug_log "Checkpointing a pod on $UPGRADE_FROM, before the upgrade..."

    local spec
    spec=$(cmd_pod_spec "alpine:latest" "$(cat "$WORKLOADS"/count-loop.sh)")
    kubectl apply -f "$spec" &&
        UPGRADE_POD=$(get_created_pod "$spec" "$NAMESPACE" 30) &&
        validate_pod "$UPGRADE_POD" 120 &&
        wait_for_log_trigger "$UPGRADE_POD" "COUNT 10" 60 || return 1
    UPGRADE_COUNT=$(last_count "$UPGRADE_POD")
    UPGRADE_ACTION_ID=$(checkpoint_pod "$(get_pod_id "$UPGRADE_POD" "$NAMESPACE")") &&
        validate_action_id "$UPGRADE_ACTION_ID" &&
        poll_action_status "$UPGRADE_ACTION_ID" "checkpoint" 180 &&
        kubectl delete pod "$UPGRADE_POD" -n "$NAMESPACE" --wait=true || return 1
    export UPGRADE_FROM UPGRADE_POD UPGRADE_COUNT UPGRADE_ACTION_ID

    debug_log "Upgrading from $UPGRADE_FROM to this chart..."
    # The released chart's helper installed its own plugins over this build's
    if [ "${CEDANA_PLUGINS_BUILDS:-}" = "local" ]; then
        install_local_plugins || return 1
    fi
    helm_install_cedana "$cluster_id" "$namespace" &&
        kubectl rollout status daemonset/cedana-helper -n "$namespace" --timeout=5m
}

helm_is_cedana_installed() {
    local namespace="$1"

    helm status cedana -n "$namespace" &>/dev/null
    return $?
}

helm_uninstall_cedana() {
    local namespace="$1"

    debug_log "Uninstalling helm chart..."

    if ! helm_is_cedana_installed "$namespace"; then
        debug_log "Helm chart is not installed"
        return 0
    fi

    helm uninstall cedana -n "$namespace" --wait --timeout=2m --ignore-not-found || {
        error_log "Failed to uninstall helm chart"
        return 1
    }

    debug_log "Waiting for all pods in $namespace namespace to terminate..."

    wait_for_cmd_fail 120 "kubectl get pods -n $namespace --no-headers 2>/dev/null | grep -q ."
    wait_for_cmd_fail 30 "kubectl get namespaces | grep -q $namespace"

    debug_log "Helm chart uninstalled successfully"
}
