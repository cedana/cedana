#!/usr/bin/env bash

####################
### K3s Provider ###
####################
#
# Local K3s cluster for CI/development testing.
# Creates a fresh K3s cluster on setup and tears it down completely.
#
# K3S_CONTAINERD selects the containerd k3s runs on:
#   external (default) - the suite's own containerd (see containerd.bash)
#   embedded           - k3s's bundled containerd, as on a stock k3s install
#
# K3S_VERSION is the k3s release to run (each bundles its own containerd), and
# CONTAINERD_VERSION the containerd release to run as the external one (default: the
# test image's).
#

export K3S_CONTAINERD="${K3S_CONTAINERD:-external}"
export K3S_VERSION="${K3S_VERSION:-v1.34.2+k3s1}"
export CONTAINERD_VERSION="${CONTAINERD_VERSION:-}"
export CONTAINERD_NAMESPACE="k8s.io"
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

# overlayfs, k3s's default and what real nodes use, can't keep its state on an overlay
# filesystem (e.g. the test container's root), so CI bind-mounts the state dirs from the
# runner's disk. Without that, fall back to native, which keeps a full copy of every layer
# of every image (many times their size), and gives pods no overlay rootfs.
k3s_snapshotter() {
    local dir=$1
    mkdir -p "$dir"
    if [ "$(stat -f -c %T "$dir" 2>/dev/null)" = "overlayfs" ]; then
        echo native
    else
        echo overlayfs
    fi
}
if [ "$K3S_CONTAINERD" = "embedded" ]; then
    K3S_SNAPSHOTTER=$(k3s_snapshotter /var/lib/rancher)
else
    K3S_SNAPSHOTTER=$(k3s_snapshotter /var/lib/containerd)
fi
export K3S_SNAPSHOTTER

START_K3S_SERVER="server \
        --write-kubeconfig-mode=644 \
        --disable=traefik \
        --snapshotter=$K3S_SNAPSHOTTER"

case "$K3S_CONTAINERD" in
external)
    START_K3S_SERVER="$START_K3S_SERVER --container-runtime-endpoint=$CONTAINERD_ADDRESS"
    ;;
embedded)
    export CONTAINERD_ADDRESS="/run/k3s/containerd/containerd.sock"
    K3S_CONTAINERD_TEMPLATE="/var/lib/rancher/k3s/agent/etc/containerd/config-v3.toml.tmpl"
    ;;
*)
    error_log "Unknown K3S_CONTAINERD=$K3S_CONTAINERD (expected external or embedded)"
    return 1
    ;;
esac

export GPU_OPERATOR_NAMESPACE="${GPU_OPERATOR_NAMESPACE:-gpu-operator}"
export GPU_OPERATOR_VERSION="${GPU_OPERATOR_VERSION:-v25.10}"

# Function to set up k3s cluster
setup_cluster() {
    debug_log "Installing k3s cluster..."

    check_env CONTAINERD_ADDRESS
    check_env CONTAINERD_CONFIG_PATH
    check_env CONTAINERD_NAMESPACE

    download_k3s
    if [ "$K3S_CONTAINERD" = "external" ] && [ -n "$CONTAINERD_VERSION" ]; then
        download_containerd "$CONTAINERD_VERSION"
    fi
    install_containerd_plugins
    configure_containerd_runtime
    # The shim inherits containerd's environment, and env beats the host config file the
    # helper writes (/etc/cedana). Keep the suite's CEDANA_URL/CEDANA_AUTH_TOKEN out of it so
    # the shim follows the helper's config, which is what points at an in-cluster propagator.
    # An embedded containerd is started by k3s, so k3s must not see them either.
    if [ "$K3S_CONTAINERD" = "embedded" ]; then
        (
            unset CEDANA_URL CEDANA_AUTH_TOKEN OTEL_EXPORTER_OTLP_ENDPOINT OTEL_EXPORTER_OTLP_HEADERS
            start_cluster
        )
        preload_all_images
    else
        (
            unset CEDANA_URL CEDANA_AUTH_TOKEN OTEL_EXPORTER_OTLP_ENDPOINT OTEL_EXPORTER_OTLP_HEADERS
            start_containerd
        )
        preload_all_images
        start_cluster
    fi

    mkdir -p ~/.kube
    cat $KUBECONFIG >~/.kube/config

    if [ "${GPU:-0}" = "1" ]; then
        setup_gpu_operator
    fi

    debug_log "k3s cluster is ready"
}

preload_all_images() {
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
    if [ -n "$PROPAGATOR_DIGEST" ]; then
        preload_images "$PROPAGATOR_REPO@$PROPAGATOR_DIGEST"
    elif [ -n "$PROPAGATOR_TAG" ]; then
        preload_images "$PROPAGATOR_REPO:$PROPAGATOR_TAG"
    fi
    if [ "${GPU:-0}" = "1" ]; then
        preload_images "$GPU_WORKLOAD_IMAGE"
        preload_images "cedana/cedana-samples:cuda"
    fi
}

# Teardown k3s cluster completely
teardown_cluster() {
    debug_log "Tearing down k3s cluster..."

    if command -v k3s-uninstall.sh &>/dev/null; then
        debug_log "Running k3s uninstall script..."
        timeout 120 k3s-uninstall.sh || error_log "k3s uninstall script timed out or failed"
    fi

    debug_log "Stopping k3s processes..."
    pkill k3s || true
    pkill kubectl || true

    sleep 2

    debug_log "Cleaning up k3s data..."
    rm -rf /var/lib/rancher/k3s || true
    rm -rf /etc/rancher/k3s || true

    stop_containerd

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
    aarch64 | arm64)
        binary_name="k3s-arm64"
        ;;
    *)
        error_log "Unsupported architecture: $arch. Only x86_64 and arm64/aarch64 are supported."
        return 1
        ;;
    esac

    wget -q "https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION//+/%2B}/$binary_name" -O /usr/local/bin/k3s
    chmod +x /usr/local/bin/k3s

    info_log "Running $(k3s --version | head -n 1)"
}

# Installs a containerd release over the test image's (/usr/local/bin is ahead in PATH)
download_containerd() {
    local version="${1#v}"
    local arch
    case "$(uname -m)" in
    aarch64 | arm64) arch=arm64 ;;
    *) arch=amd64 ;;
    esac

    debug_log "Downloading containerd $version..."
    curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 \
        "https://github.com/containerd/containerd/releases/download/v$version/containerd-$version-linux-$arch.tar.gz" |
        tar -xz -C /usr/local bin/containerd bin/containerd-shim-runc-v2 bin/ctr || {
        error_log "Failed to download containerd $version"
        return 1
    }

    local running
    running=$(containerd --version | awk '{print $3}')
    if [ "${running#v}" != "$version" ]; then
        error_log "containerd $version was installed, but $(command -v containerd) is $running"
        return 1
    fi
    info_log "Running containerd $version"
}

configure_containerd_runtime() {
    debug_log "Configuring containerd runtime for k3s..."

    if [ "$K3S_CONTAINERD" = "embedded" ]; then
        # With a GPU, as on a stock k3s GPU node, k3s finds nvidia-container-runtime in PATH
        # and adds the nvidia runtime and RuntimeClass itself (checked in start_cluster), which
        # the GPU operator's pods use, as with the external containerd.
        # k3s regenerates its containerd config on every start, so extend it through
        # the template (config version 3, as k3s ships containerd 2.x), whose base keeps
        # the runtimes k3s detects
        debug_log "Writing containerd config template to $K3S_CONTAINERD_TEMPLATE"
        mkdir -p "$(dirname "$K3S_CONTAINERD_TEMPLATE")"
        cat >"$K3S_CONTAINERD_TEMPLATE" <<'END_CAT'
{{ template "base" . }}

[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes."cedana"]
  runtime_type = "io.containerd.runc.v2"
  runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
END_CAT
        debug_log "Configured containerd runtime for k3s"
        return 0
    fi

    check_env CONTAINERD_CONFIG_PATH

    local path=$CONTAINERD_CONFIG_PATH

    # The test image's containerd config uses native, for its overlay root
    if [ "$K3S_SNAPSHOTTER" != "native" ]; then
        debug_log "Switching containerd to the $K3S_SNAPSHOTTER snapshotter"
        sed -i "s/snapshotter = \"native\"/snapshotter = \"$K3S_SNAPSHOTTER\"/" \
            "$CONTAINERD_CONFIG_PATH" "$(dirname "$CONTAINERD_CONFIG_PATH")"/conf.d/*.toml 2>/dev/null || true
    fi

    if [ "${GPU:-0}" = "1" ]; then
        local config_dir
        config_dir=$(dirname "$CONTAINERD_CONFIG_PATH")/conf.d
        path="$config_dir/999-cedana.toml"
        cp "$config_dir/99-nvidia.toml" "$path"
    fi

    # Already there when the suite runs again in the same job (e.g. the upgrade suite)
    if grep -q 'runtimes."cedana"' "$path"; then
        debug_log "Containerd runtime for k3s already configured in $path"
        return 0
    fi

    debug_log "Writing containerd config to $path"
    cat >>"$path" <<'END_CAT'
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes."cedana"]
    runtime_type = "io.containerd.runc.v2"
    runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
[plugins."io.containerd.cri.v1.images"]
  use_local_image_pull = true
END_CAT

    debug_log "Configured containerd runtime for k3s"
}

start_cluster() {
    debug_log "Starting k3s cluster..."

    if ! command -v k3s &>/dev/null; then
        error_log "k3s binary not found"
        return 1
    fi

    debug_log "k3s binary found, starting k3s..."
    eval "k3s $START_K3S_SERVER" &

    debug_log "Waiting for k3s cluster to start..."

    local timeout=300
    wait_for_cmd "$timeout" "kubectl get nodes | grep -q ." || {
        error_log "Timed out waiting for the k3s node to register"
        return 1
    }

    debug_log "Waiting for k3s node to be Ready..."
    if ! kubectl wait --for=condition=Ready node --all --timeout="$timeout"s; then
        error_log "Timed out waiting for k3s node to be Ready"
        return 1
    fi

    if [ "$K3S_CONTAINERD" = "embedded" ] && [ "${GPU:-0}" = "1" ]; then
        local config=/var/lib/rancher/k3s/agent/etc/containerd/config.toml
        if ! grep -q nvidia-container-runtime "$config"; then # its BinaryName, as k3s writes it
            error_log "k3s did not set up the nvidia runtime (is nvidia-container-runtime in PATH?): $config"
            grep -n "runtimes" "$config" >&2 || true
            return 1
        fi
    fi

    debug_log "k3s cluster has started successfully"
}

stop_cluster() {
    debug_log "Stopping k3s cluster..."

    if command -v k3s-killall.sh &>/dev/null; then
        debug_log "Running k3s killall script..."
        timeout 120 k3s-killall.sh || error_log "k3s killall script timed out or failed"
    fi

    pkill k3s || true

    debug_log "k3s cluster stopped"
}

# Pre-load an image into k3s from docker if available locally
preload_images() {
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

    # docker's containerd image store saves a multi-platform index, which ctr 2.x
    # refuses to unpack unless told which platform to use. --local makes it honor
    # --platform instead of deferring to the transfer service's (empty) config.
    local platform
    case "$(uname -m)" in
    aarch64 | arm64) platform=linux/arm64 ;;
    *) platform=linux/amd64 ;;
    esac
    ctr -n $CONTAINERD_NAMESPACE --address "$CONTAINERD_ADDRESS" images import --local --platform "$platform" --snapshotter "$K3S_SNAPSHOTTER" "$tar"
    rm -f "$tar"

    ctr -n $CONTAINERD_NAMESPACE --address "$CONTAINERD_ADDRESS" images tag docker.io/"$image" docker.io/"$digest_ref"

    debug_log "Preloaded image $image into k3s"
}

setup_gpu_operator() {
    debug_log "Installing NVIDIA GPU operator..."

    helm repo add nvidia https://helm.ngc.nvidia.com/nvidia &&
        helm repo update

    mount --make-rshared /

    # See https://github.com/NVIDIA/gpu-operator/issues/569
    # Because we are working inside a container.
    cat <<END_CAT >/tmp/gpu-operator-values.yaml
validator:
  driver:
    env:
    - name: DISABLE_DEV_CHAR_SYMLINK_CREATION
      value: "true"
END_CAT

    debug helm upgrade -i --wait gpu-operator -n "$GPU_OPERATOR_NAMESPACE" --create-namespace \
        nvidia/gpu-operator --version="$GPU_OPERATOR_VERSION" \
        --set cdi.enabled=false --set driver.enabled=false --set toolkit.enabled=false \
        -f /tmp/gpu-operator-values.yaml

    # NOTE: Assuming driver is already installed on the k3s node

    wait_for_ready "$GPU_OPERATOR_NAMESPACE" 120
    wait_for_cmd 120 is_gpu_available 1

    debug_log "NVIDIA GPU operator installed successfully"
}

# Optional interface functions (no-ops for k3s)
create_nodegroup() {
    debug_log "K3s provider: nodegroups not supported"
}

delete_nodegroup() {
    debug_log "K3s provider: nodegroups not supported"
}
