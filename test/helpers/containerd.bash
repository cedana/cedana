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
# shellcheck disable=SC2034 # used by test/regression/plugins/containerd.bats
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

# What containerd's script/setup/install-cni installs (the CNI plugins at the version in go.mod's
# module graph, and its containerd-net config), but from the plugins' release instead of a clone
# built from source (~40 CPU-seconds of go build on every K3s job)
install_containerd_plugins() {
    debug_log "Installing CNI plugins..."
    local version arch
    version=$(go list -f '{{.Version}}' -m github.com/containernetworking/plugins) || return 1
    case "$(uname -m)" in
    aarch64 | arm64) arch=arm64 ;;
    *) arch=amd64 ;;
    esac
    mkdir -p /opt/cni/bin /etc/cni/net.d
    curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 \
        "https://github.com/containernetworking/plugins/releases/download/$version/cni-plugins-linux-$arch-$version.tgz" |
        tar -xz -C /opt/cni/bin || {
        error_log "Failed to download CNI plugins $version"
        return 1
    }
    cat >/etc/cni/net.d/10-containerd-net.conflist <<'EOF'
{
  "cniVersion": "1.0.0",
  "name": "containerd-net",
  "plugins": [
    {
      "type": "bridge",
      "bridge": "cni0",
      "isGateway": true,
      "ipMasq": true,
      "promiscMode": true,
      "ipam": {
        "type": "host-local",
        "ranges": [
          [{
            "subnet": "10.88.0.0/16"
          }],
          [{
            "subnet": "2001:4860:4860::/64"
          }]
        ],
        "routes": [
          { "dst": "0.0.0.0/0" },
          { "dst": "::/0" }
        ]
      }
    },
    {
      "type": "portmap",
      "capabilities": {"portMappings": true}
    }
  ]
}
EOF
    debug_log "Installed CNI plugins $version"
}
