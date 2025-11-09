#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

pull_images() {
    if ! cmd_exists containerd; then
        error_log "containerd is not installed. Please install it first."
        exit 1
    fi

    ctr image pull docker.io/library/alpine:latest
    ctr image pull docker.io/library/nginx:latest
    # Add more images as needed
}

pull_latest_cedana_samples_image() {
    local latest_tag
    local snapshotter="native"
    
    latest_tag=$(get_latest_cedana_samples_tag)
    echo "Pulling latest cedana-samples image with tag: $latest_tag"

    ctr images pull --snapshotter "$snapshotter" "docker.io/cedana/cedana-samples:${latest_tag}"
    debug_log "Verifying pulled image..."
    ctr images ls | grep cedana-samples || true
}

get_latest_cedana_samples_tag() {
    local repo="cedana/cedana-samples"
    local tag="cuda12.4-torch2.7"
    local json
    local parsed

    if ! cmd_exists jq; then
        debug_log "jq is not installed. Using default tag: $tag"
        echo "$tag"
        return 0
    fi

    if ! json=$(curl -fsSL --connect-timeout 5 --max-time 10 \
            "https://hub.docker.com/v2/repositories/${repo}/tags?page_size=100"); then
        debug_log "curl failed; using default tag: $tag"
        echo "$tag"
        return 0
    fi

    parsed=$(printf '%s' "$json" \
        | jq -r '.results[] | select(.name | startswith("cuda")) | .name' \
        | sort -V | tail -n1)

    if [ -z "$parsed" ] || [ "$parsed" = "null" ]; then
        debug_log "no cuda tag parsed; using default tag: $tag"
        echo "$tag"
    else
        echo "$parsed"
    fi
}

start_containerd() {
    if pgrep -x contianerd > /dev/null; then
        debug_log "containerd is already running."
        for i in {1..30}; do
            if ctr version > /dev/null 2>&1; then
                debug_log "containerd is responsive."
                if [ -f /etc/containerd/config.toml ]; then
                    snapshotter=$(grep -m 1 'snapshotter = ' /etc/containerd/config.toml | cut -d'"' -f2)
                    debug_log "Containerd is using snapshotter: $snapshotter"
                fi
                return 0
            fi
            sleep 1
        done
        error_log "containerd is running but socket is not responsive."
        exit 1
    fi
    if cmd_exists start-docker.sh; then
        debug_log "Starting containerd via start-docker.sh"
        start-docker.sh # XXX: use docker-in-docker, if available, from the container image
        return 0
    fi
    if cmd_exists containerd; then
        debug_log "Starting containerd manually"
        if [ ! -f /etc/containerd/config.toml ]; then
            mkdir -p /etc/containerd
            containerd config default > /etc/containerd/config.toml
            sed -i 's/snapshotter = "overlayfs"/snapshotter = "native"/g' /etc/containerd/config.toml
        fi
        containerd > /var/log/containerd.log 2>&1 &

        for i in {1..30}; do
            if ctr version > /dev/null 2>&1; then
                debug_log "containerd started and is responsive."
            fi
            sleep 1
        done
        error_log "containerd failed to start or is not responsive."
        cat /var/log/containerd.log
        return 1
    else
        error_log "containerd is not installed. Please install it first."
        exit 1
    fi
}

stop_containerd() {
    if cmd_exists start-docker.sh; then
        pkill supervisord
    elif pid=$(pidof containerd); then
        kill "$pid"
    else
        debug_log "containerd is not running."
    fi
}
