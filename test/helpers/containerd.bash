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
    # Check if containerd is already running
    if pidof containerd > /dev/null 2>&1; then
        debug_log "containerd is already running."
        return 0
    fi

    if cmd_exists start-docker.sh; then
        start-docker.sh # XXX: use docker-in-docker, if available, from the container image
    elif cmd_exists containerd; then
        containerd > /dev/null &
    else
        error_log "containerd is not installed. Please install it first."
        exit 1
    fi
}

stop_containerd() {
    if cmd_exists start-docker.sh; then
        pkill containerd
    elif pid=$(pidof containerd); then
        kill "$pid"
    else
        debug_log "containerd is not running."
    fi
}
