#!/usr/bin/env bash

# Helper functions that hit the local Cedana API

function start_cedana() {
    ./build-start-daemon.sh --no-build --args="$@"
}

function stop_cedana() {
    ./reset.sh
}

function exec_task() {
    local task="$1"
    local job_id="$2"
    shift 2
    cedana exec -w "$DIR" "$task" -i "$job_id" $@
}

function checkpoint_task() {
    local job_id="$1"
    shift 1
    cedana dump job "$job_id" -d /tmp $@
}

function restore_task() {
    local job_id="$1"
    shift 1
    cedana restore job "$job_id" $@
}

function start_busybox(){
    local container_name="$1"

    sudo ctr image pull docker.io/library/busybox:latest

    sudo ctr run -d docker.io/library/busybox:latest "$container_name" sh -c 'while true; do sleep 3600; done'
}

function start_jupyter_notebook(){
    local container_name="$1"
    local seccomp_profile_path="./test/regression/iouring-profile.json"

    echo "Pulling the Docker image..."
    pull_output=$(sudo ctr image pull docker.io/cedana/jupyter-base:latest)
    echo "$pull_output"

    echo "Running the container..."
    pwd
    run_output=$(sudo ctr run --seccomp --seccomp-profile="$seccomp_profile_path" -d docker.io/cedana/jupyter-base:latest "$container_name")
    echo "$run_output"
}

function start_sleeping_jupyter_notebook(){
    local image_ref="$1"
    local container_name="$2"

    sudo ctr run -d "$image_ref" "$container_name" sh -c 'while true; do sleep 3600; done'
}

function rootfs_checkpoint() {
    local container_id="$1"
    local image_ref="$2"
    local containerd_sock="$3"
    local namespace="$4"

    cedana dump rootfs -p "$container_id" --ref "$image_ref" -a "$containerd_sock" -n "$namespace"
}

function containerd_checkpoint() {
    local container_id="$1"
    local image_ref="$2"
    local containerd_sock="$3"
    local namespace="$4"
    local dir="$5"

    cedana dump containerd --id "$container_id" --ref "$image_ref" -a "$containerd_sock" -n "$namespace" --dir "$dir" --root "/run/containerd/runc/default"
}

function rootfs_restore() {
    local container_id="$1"
    local image_ref="$2"
    local containerd_sock="$3"
    local namespace="$4"

    cedana restore rootfs -p "$container_id" --ref "$image_ref" -a "$containerd_sock" -n "$namespace"
}

function runc_checkpoint() {
    local dir="$1"
    local job_id="$2"
    shift 2
    cedana dump runc --dir "$dir" --id "$job_id" $@
}
# Bundle for jupyter notebook restore
# /run/containerd/io.containerd.runtime.v2.task/default/jupyter-notebook-restore
function runc_restore() {
    local bundle="$1"
    local dir="$2"
    local id="$3"
    local tty="$4"
    cedana restore runc -e -b "$bundle" --dir "$dir" --id "$id" --console-socket "$tty"
}

function runc_restore_jupyter() {
    local bundle="$1"
    local dir="$2"
    local id="$3"
    local pid="$4"
    cedana restore runc -e -b "$bundle" --dir "$dir" --id "$id"
}

function fail() {
    echo "$@" >&2
    exit 1
}
