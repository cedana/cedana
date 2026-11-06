#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

export CONTAINERD_CONFIG_PATH="/etc/containerd/config.toml"
export CONTAINERD_ADDRESS="/run/containerd/containerd.sock"
export CONTAINERD_SNAPSHOTTER="native"
# Where the suite's own containerd logs (its shims, e.g. cedana-shim-runc-v2, log through it),
# kept for the CI diagnostics (see collect_diagnostics)
export CONTAINERD_LOG="${CONTAINERD_LOG:-/tmp/containerd.log}"

# Command for the nginx image that listens on $NGINX_PORT. The stock image ignores
# NGINX_PORT (it only substitutes env into /etc/nginx/templates), so containers
# sharing a network namespace (e.g. the host's, with `cedana run containerd`)
# would otherwise all race for :80.
NGINX_CMD=(sh -c 'sed -i -E "s/(listen[[:space:]]+(\[::\]:)?)80;/\1$NGINX_PORT;/" /etc/nginx/conf.d/default.conf && exec nginx -g "daemon off;"')

pull_images() {
    if ! cmd_exists containerd; then
        error_log "containerd is not installed. Please install it first."
        exit 1
    fi

    # NOTE: --local is required, as the (default) transfer-service pull fails
    # with "no unpack platforms defined" on containerd 2.x with our config,
    # and it also does not honor --snapshotter for unpacking
    ctr image pull --local --snapshotter "$CONTAINERD_SNAPSHOTTER" docker.io/library/alpine:latest
    ctr image pull --local --snapshotter "$CONTAINERD_SNAPSHOTTER" docker.io/library/nginx:latest
    # Add more images as needed
}

start_containerd() {
    debug_log "Starting containerd..."

    if pidof containerd > /dev/null 2>&1; then
        debug_log "Containerd is already running"
        return 0
    fi

    if cmd_exists containerd; then
        containerd -c $CONTAINERD_CONFIG_PATH -a $CONTAINERD_ADDRESS >> "$CONTAINERD_LOG" 2>&1 &
    else
        error_log "Containerd is not installed. Please install it first."
        exit 1
    fi

    wait_for_cmd 30 ctr --address $CONTAINERD_ADDRESS version

    debug_log "Containerd started"
}

stop_containerd() {
    debug_log "Stopping containerd..."

    if pid=$(pidof containerd); then
        kill "$pid"
    else
        debug_log "Containerd is not running"
    fi

    pkill -f containerd-shim-runc-v2 || true
    pkill -f cedana-shim-runc-v2 || true

    wait_for_cmd_fail 30 ctr --address $CONTAINERD_ADDRESS version

    debug_log "Containerd stopped"
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

install_containerd_plugins() {
    debug_log "Installing CNI plugins..."
    curl -sSL https://raw.githubusercontent.com/containerd/containerd/refs/heads/main/script/setup/install-cni | bash
    debug_log "Installed CNI plugins"
}
