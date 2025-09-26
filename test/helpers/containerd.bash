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

start_containerd() {
    if cmd_exists entrypoint.sh; then
        entrypoint.sh # XXX: use docker-in-docker, if available, from the container image
    elif cmd_exists containerd; then
        containerd > /dev/null &
    else
        error_log "containerd is not installed. Please install it first."
        exit 1
    fi
}

stop_containerd() {
    if pid=$(pidof containerd); then
        kill "$pid"
    else
        debug_log "containerd is not running."
    fi
}
