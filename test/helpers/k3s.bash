#!/usr/bin/env bash

###################
### K3s Helpers ###
###################

CONTAINERD_CONFIG_PATH="/var/lib/rancher/k3s/agent/etc/containerd/config.toml"
export CONTAINERD_ADDRESS="/run/k3s/containerd/containerd.sock"
CONTAINERD_NAMESPACE="k8s.io"
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
INSTALL_K3S_EXEC="server \
        --write-kubeconfig-mode=644 \
        --disable=traefik \
        --snapshotter=native"

# Function to set up k3s cluster
setup_cluster() {
    debug_log "Installing k3s cluster..."

    download_k3s
    start_cluster

    # XXX: The tar in busybox is incompatible with CRIU
    rm /var/lib/rancher/k3s/data/current/bin/tar

    mkdir -p ~/.kube
    cat $KUBECONFIG > ~/.kube/config

    if [ -n "$CONTROLLER_DIGEST" ]; then
        preload_images "$CONTROLLER_REPO@$CONTROLLER_DIGEST"
    elif [ -n "$CONTROLLER_TAG" ]; then
        preload_images "$CONTROLLER_REPO:$CONTROLLER_TAG"
    fi
    if [ -n "$HELPER_DIGEST" ]; then
        preload_images "$HELPER_REPO@$HELPER_DIGEST"
    elif [ -n "$HELPER_TAG" ]; then
        preload_images "$HELPER_REPO:$HELPER_TAG"
    fi

    debug_log "k3s cluster is ready"
}

start_cluster() {
    debug_log "Starting k3s cluster..."

    configure_containerd_runtime

    if ! command -v k3s &> /dev/null; then
        error_log "k3s binary not found"
        return 1
    fi

    debug_log "k3s binary found, starting k3s..."
    eval "k3s $INSTALL_K3S_EXEC" &> /dev/null &

    debug_log "Waiting for k3s cluster to start..."

    local timeout=300
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
    pkill -f containerd-shim-runc-v2 || true
    pkill -f cedana-shim-runc-v2 || true
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

download_k3s() {
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

configure_containerd_runtime() {
    debug_log "Configuring containerd runtime for k3s..."

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
[plugins.'io.containerd.cri.v1.images']
  use_local_image_pull = true
END_CAT
    fi

    debug_log "Configured containerd runtime for k3s"
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

    ctr -n $CONTAINERD_NAMESPACE --address "$CONTAINERD_ADDRESS" images import "$tar"
    rm -f "$tar"

    ctr -n $CONTAINERD_NAMESPACE --address "$CONTAINERD_ADDRESS" images tag docker.io/"$image" docker.io/"$digest_ref"

    debug_log "Preloaded image $image into k3s"
}
