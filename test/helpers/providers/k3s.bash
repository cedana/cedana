#!/usr/bin/env bash

###################
### K3s Provider ###
###################
#
# Local K3s cluster for CI/development testing.
# Creates a fresh K3s cluster on setup and tears it down completely.
#
# Environment variables:
#   CONTAINERD_ADDRESS - Containerd socket (default: /run/k3s/containerd/containerd.sock)
#

CONTAINERD_CONFIG_PATH="/var/lib/rancher/k3s/agent/etc/containerd/config.toml"
export CONTAINERD_ADDRESS="${CONTAINERD_ADDRESS:-/run/k3s/containerd/containerd.sock}"
CONTAINERD_NAMESPACE="k8s.io"
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
INSTALL_K3S_EXEC="server \
        --write-kubeconfig-mode=644 \
        --disable=traefik \
        --snapshotter=native"

_download_k3s() {
    debug_log "Downloading k3s binary..."

    local arch
    arch=$(uname -m)
    local binary_name

    case "$arch" in
        x86_64)
            binary_name="k3s"
            ;;
        aarch64|arm64)
            binary_name="k3s-arm64"
            ;;
        *)
            error_log "Unsupported architecture: $arch. Only x86_64 and arm64/aarch64 are supported."
            return 1
            ;;
    esac

    wget "https://github.com/k3s-io/k3s/releases/download/v1.34.2%2Bk3s1/$binary_name" -O /usr/local/bin/k3s
    chmod +x /usr/local/bin/k3s

    debug_log "Downloaded k3s binary"
}

_preinstall_containerd_runtime() {
    debug_log "Pre-installing containerd runtime for k3s..."

    if ! path_exists /usr/local/bin/cedana-shim-runc-v2; then
        if [ "$CEDANA_PLUGINS_BUILDS" = "local" ]; then
            error_log "Runtime shim not found in /usr/local/bin"
            return 1
        else
            cedana plugin install containerd/runtime-runc
        fi
    fi

    if ! path_exists $CONTAINERD_CONFIG_PATH; then
        mkdir -p "$(dirname "$CONTAINERD_CONFIG_PATH")"
        touch "$CONTAINERD_CONFIG_PATH"
    fi

    local template=$CONTAINERD_CONFIG_PATH.tmpl
    if ! grep -q 'cedana' "$template" 2>/dev/null; then
        echo '{{ template "base" . }}' > $template
        cat >> $template <<'END_CAT'
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes."cedana"]
    runtime_type = "io.containerd.runc.v2"
    runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
[plugins.'io.containerd.cri.v1.images']
  use_local_image_pull = true
END_CAT
    fi

    debug_log "Pre-installed containerd runtime for k3s"
}

_start_cluster() {
    debug_log "Starting k3s cluster..."

    # Pre-install the containerd v2 runtime so we won't have to restart k3s
    _preinstall_containerd_runtime

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

_stop_cluster() {
    debug_log "Stopping k3s cluster..."

    if command -v k3s-killall.sh &>/dev/null; then
        debug_log "Running k3s killall script..."
        timeout 120 k3s-killall.sh || error_log "k3s killall script timed out or failed"
    fi

    debug_log "Stopping k3s processes..."
    pkill k3s || true
    pkill -f containerd-shim-runc-v2 || true
    pkill -f cedana-shim-runc-v2 || true
    pkill kubectl || true

    sleep 2

    debug_log "k3s cluster stopped"
}

# Pre-load an image into k3s from docker if available locally
_preload_images() {
    local image="$1"
    if ! docker images -q "$image" 2>/dev/null | grep -q .; then
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

    ctr -n $CONTAINERD_NAMESPACE --address "$CONTAINERD_ADDRESS" images import "$tar"
    rm -f "$tar"

    ctr -n $CONTAINERD_NAMESPACE --address "$CONTAINERD_ADDRESS" images tag docker.io/"$image" docker.io/"$digest_ref"

    debug_log "Preloaded image $image into k3s"
}

setup_cluster() {
    debug_log "Installing k3s cluster..."

    _download_k3s
    _start_cluster

    # XXX: The tar in busybox is incompatible with CRIU
    rm -f /var/lib/rancher/k3s/data/current/bin/tar

    mkdir -p ~/.kube
    cat $KUBECONFIG > ~/.kube/config

    # Preload controller/helper images if available locally
    if [ -n "$CONTROLLER_DIGEST" ]; then
        _preload_images "$CONTROLLER_REPO@$CONTROLLER_DIGEST"
    elif [ -n "$CONTROLLER_TAG" ]; then
        _preload_images "$CONTROLLER_REPO:$CONTROLLER_TAG"
    fi
    if [ -n "$HELPER_DIGEST" ]; then
        _preload_images "$HELPER_REPO@$HELPER_DIGEST"
    elif [ -n "$HELPER_TAG" ]; then
        _preload_images "$HELPER_REPO:$HELPER_TAG"
    fi

    debug_log "k3s cluster is ready"
}

teardown_cluster() {
    debug_log "Tearing down k3s cluster..."

    if command -v k3s-uninstall.sh &>/dev/null; then
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

# Optional interface functions (no-ops for k3s)
create_nodegroup() {
    debug_log "K3s provider: nodegroups not supported"
}

delete_nodegroup() {
    debug_log "K3s provider: nodegroups not supported"
}
