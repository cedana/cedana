#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

start_containerd() {
    if ! cmd_exists containerd; then
        error_log "containerd is not installed. Please install it first."
        exit 1
    fi

    containerd > /dev/null &
}

stop_containerd() {
    if pid=$(pidof containerd); then
        kill -9 "$pid"
    else
        debug_log "containerd is not running."
    fi
}
