#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

start_containerd() {
    if ! cmd_exists containerd; then
        echo "containerd is not installed. Please install it first."
        exit 1
    fi

    containerd &
}

stop_containerd() {
    if pid=$(pidof containerd); then
        kill -9 "$pid"
    else
        echo "containerd is not running."
    fi
}
