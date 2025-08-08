#!/usr/bin/env bash

##################################
### K3s and Helm Setup Helpers ###
##################################

export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
INSTALL_K3S_EXEC="server \
        --write-kubeconfig-mode=644 \
        --disable=traefik \
        --snapshotter=native"
CONTAINERD_CONFIG_PATH="/var/lib/rancher/k3s/agent/etc/containerd/config.toml"
CONTAINERD_SOCK_PATH="/run/k3s/containerd/containerd.sock"
CONTAINERD_NAMESPACE="k8s.io"

# Function to set up k3s cluster
setup_cluster() {
    debug_log "Installing k3s cluster..."

    wget https://get.k3s.io -O /tmp/k3s-install.sh
    chmod +x /tmp/k3s-install.sh

    if ! /tmp/k3s-install.sh &> /dev/null; then
        debug_log "Installer failed, will try the binary directly"
        start_cluster
    fi

    # XXX: The tar in busybox is incompatible with CRIU
    rm /var/lib/rancher/k3s/data/current/bin/tar

    mkdir -p ~/.kube
    cat $KUBECONFIG > ~/.kube/config

    if [ -n "$CONTROLLER_DIGEST" ]; then
        preload_images "cedana/cedana-controller@$CONTROLLER_DIGEST"
    elif [ -n "$CONTROLLER_TAG" ]; then
        preload_images "cedana/cedana-controller:$CONTROLLER_TAG"
    fi
    if [ -n "$HELPER_DIGEST" ]; then
        preload_images "cedana/cedana-helper@$HELPER_DIGEST"
    elif [ -n "$HELPER_TAG" ]; then
        preload_images "cedana/cedana-helper:$HELPER_TAG"
    fi

    debug_log "k3s cluster is ready"
}

start_cluster() {
    debug_log "Starting k3s cluster..."

    # XXX: Pre-install the runtime shim so we won't have to restart k3s otherwise it needs to
    # be restarted after Cedana installs the new runtime shim.

    install_runtime_shim

    if ! command -v k3s &> /dev/null; then
        error_log "k3s binary not found"
        return 1
    fi

    debug_log "k3s binary found, starting k3s..."
    eval "k3s $INSTALL_K3S_EXEC" &> /dev/null &

    debug_log "Waiting for k3s cluster to start..."

    local timeout=120
    wait_for_cmd "$timeout" "kubectl get nodes | grep -q ."

    debug_log "Waiting for k3s node to be Ready..."
    if ! kubectl wait --for=condition=Ready node --all --timeout="$timeout"s; then
        error_log "Timed out waiting for k3s node to be Ready"
        return 1
    fi

    debug_log "k3s cluster has started successfully"
}

stop_cluster() {
    debug_log "Stopping k3s cluster..."

    if command -v k3s-killall.sh; then
        debug_log "Running k3s killall script..."
        timeout 120 k3s-killall.sh || error_log "k3s killall script timed out or failed"
    fi

    debug_log "Stopping k3s processes..."
    pkill k3s || true
    pkill containerd-shim-runc-v2 || true
    pkill cedana-shim-runc-v2 || true
    pkill kubectl || true

    sleep 2

    debug_log "k3s cluster stopped"
}

# Teardown k3s cluster completely
teardown_cluster() {
    debug_log "Tearing down k3s cluster..."

    if command -v k3s-uninstall.sh; then
        debug_log "Running k3s uninstall script..."
        timeout 120 k3s-uninstall.sh || error_log "k3s uninstall script timed out or failed"
    fi

    debug_log "Stopping k3s processes..."
    pkill k3s || true
    pkill -f containerd-shim-runc-v2 || true
    pkill -f cedana-shim-runc-v2 || true
    pkill kubectl || true

    sleep 2

    debug_log "Cleaning up k3s data..."
    rm -rf /var/lib/rancher/k3s || true
    rm -rf /etc/rancher/k3s || true

    debug_log "k3s teardown complete"
}

install_runtime_shim() {
    debug_log "Installing runtime shim for k3s..."

    if ! path_exists /usr/local/bin/cedana-shim-runc-v2; then
        error_log "Shim not found in /usr/local/bin"
        return 1
    fi

    if ! path_exists $CONTAINERD_CONFIG_PATH; then
        mkdir -p "$(dirname "$CONTAINERD_CONFIG_PATH")"
        touch "$CONTAINERD_CONFIG_PATH"
    fi

    local template=$CONTAINERD_CONFIG_PATH.tmpl
    if ! grep -q 'cedana' "$template"; then
        echo '{{ template "base" . }}' > $template
        cat >> $template <<'END_CAT'
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes."cedana"]
    runtime_type = "io.containerd.runc.v2"
    runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
END_CAT
    fi

    debug_log "Installed runtime shim for k3s"
}

# Pre-load an image into k3s from docker if available locally
preload_images() {
    local image="$1"
    if ! docker images -q "$image" | grep -q .; then
        debug_log "Local $image image not found, skipping..."
        return 0
    fi

    local tar
    tar=/tmp/$(unix_nano).tar
    debug_log "Local $image image found, preloading..."
    docker save "$image" -o "$tar"

    local digest_ref
    digest_ref=$(docker inspect --format='{{index .RepoDigests 0}}' "$image")

    if [ -z "${digest_ref}" ]; then
      error_log "Failed to find digest for image ${image}. Skipping..."
      return 0
    fi

    ctr -n $CONTAINERD_NAMESPACE --address $CONTAINERD_SOCK_PATH images import "$tar"
    rm -f "$tar"

    ctr -n $CONTAINERD_NAMESPACE --address $CONTAINERD_SOCK_PATH images tag docker.io/"$image" docker.io/"$digest_ref"

    debug_log "Preloaded image $image into k3s"
}
