#!/usr/bin/env bash

# Helper functions that hit the local Cedana API

function start_cedana() {
    ./build-start-daemon.sh --no-build --args="$@"
}

function stop_cedana() {
    ./reset.sh
}

# start a busybox container
function start_busybox(){
    local pod_config_path="$1"
    local container_config_path="$2"

    # get pod id after creation
    local pod_id=$(crictl runp "$pod_config_path")

    # pull the docker.io/library/busybox:latest image
    crictl pull busybox:latest

    # container creation id
    local container_id=$(crictl create "$pod_id" "$container_config_path" "$pod_config_path")

    # start the container
    crictl start "$container_id"
}

function rootfs_checkpoint() {
    local container_id="$1"
    local image_ref="$2"
    local containerd_sock="$3"
    local namespace="$4"

    cedana dump rootfs -p "$container_id" --ref "$image_ref" -a "$containerd_sock" -n "$namespace"
}

function rootfs_restore() {
    local container_id="$1"
    local image_ref="$2"
    local containerd_sock="$3"
    local namespace="$4"

    cedana restore rootfs -p "$container_id" --ref "$image_ref" -a "$containerd_sock" -n "$namespace"
}

function fail() {
    echo "$@" >&2
    exit 1
}

function start_crio_no_setup() {
    crio -l debug &>"/tmp/crio.log" &
    CRIO_PID=$!
    wait_until_reachable
}

function wait_until_reachable() {
    retry 15 1 crictl info
}
