#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

pull_images() {
    if ! cmd_exists containerd; then
        error_log "containerd is not installed."
        exit 1
    fi
    # Use default socket
    ctr image pull docker.io/library/alpine:latest
    ctr image pull docker.io/library/nginx:latest
}

pull_latest_cedana_samples_image() {
    local latest_tag
    latest_tag=$(get_latest_cedana_samples_tag)
    echo "Pulling latest cedana-samples image with tag: $latest_tag"
    ctr images pull "docker.io/cedana/cedana-samples:${latest_tag}"
}

get_latest_cedana_samples_tag() {
    # Keep your existing logic here, it is fine
    local repo="cedana/cedana-samples"
    local tag="cuda12.4-torch2.7"
    if ! cmd_exists jq; then echo "$tag"; return 0; fi
    local json
    if ! json=$(curl -fsSL --connect-timeout 5 "https://hub.docker.com/v2/repositories/${repo}/tags?page_size=100"); then
        echo "$tag"; return 0;
    fi
    local parsed
    parsed=$(printf '%s' "$json" | jq -r '.results[] | select(.name | startswith("cuda")) | .name' | sort -V | tail -n1)
    if [ -z "$parsed" ] || [ "$parsed" = "null" ]; then echo "$tag"; else echo "$parsed"; fi
}

start_containerd() {
    if cmd_exists start-docker.sh; then
        start-docker.sh
    fi

    local retries=0
    echo "Waiting for containerd..."
    while ! ctr version >/dev/null 2>&1; do
        sleep 1
        retries=$((retries+1))
        if [ $retries -gt 10 ]; then
            # If standard socket isn't up, try forcing it manually 
            # pointing specifically to the SAFE VOLUME
            echo "Socket not found, starting manually on safe volume..."
            containerd \
                --config /etc/containerd/config.toml \
                --root /var/lib/containerd \
                --state /run/containerd \
                > /var/log/containerd.log 2>&1 &
            sleep 2
        fi
    done
    echo "Containerd is ready."
}

stop_containerd() {
    pkill containerd || true
    if cmd_exists start-docker.sh; then
        pkill supervisord || true
    fi
}