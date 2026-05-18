#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

export CONTAINERD_CONFIG_PATH="/etc/containerd/config.toml"
export CONTAINERD_ADDRESS="/run/containerd/containerd.sock"
export CONTAINERD_SNAPSHOTTER="native"

pull_images() {
    if ! cmd_exists containerd; then
        error_log "containerd is not installed. Please install it first."
        exit 1
    fi

    ctr image pull docker.io/library/alpine:latest
    ctr image pull docker.io/library/nginx:latest
    # Add more images as needed
}

start_containerd() {
    debug_log "Starting containerd..."

    if pidof containerd > /dev/null 2>&1; then
        debug_log "Containerd is already running"
        return 0
    fi

    if cmd_exists containerd; then
        containerd -c $CONTAINERD_CONFIG_PATH -a $CONTAINERD_ADDRESS > /dev/null &
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

install_containerd_plugins() {
    debug_log "Installing CNI plugins..."
    curl -sSL https://raw.githubusercontent.com/containerd/containerd/refs/heads/main/script/setup/install-cni | bash
    debug_log "Installed CNI plugins"
}
