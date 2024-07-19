#!/usr/bin/env bash

# Helper functions that hit the local Cedana API

function start_cedana() {
    ./build-start-daemon.sh --no-build
}

function stop_cedana() {
    ./reset.sh
}

function exec_task() {
    local task="$1"
    local job_id="$2"
    cedana exec -w "$DIR" "$task" -i "$job_id"
}

function checkpoint_task() {
    local job_id="$1"
    cedana dump job "$job_id" -d /tmp
}

function restore_task() {
    local job_id="$1"
    cedana restore job "$job_id"
}

function start_busybox(){
  local container_name="$1"

  sudo ctr image pull docker.io/library/busybox:latest

  sudo ctr run -d docker.io/library/busybox:latest "$container_name" sh -c 'while true; do sleep 3600; done'
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

function runc_checkpoint() {
  local dir="$1"
  local job_id="$2"
  cedana dump runc --dir "$dir" --id "$job_id"
}

function runc_restore() {
  local bundle="$1"
  local dir="$2"
  local id="$3"
  local tty="$4"
  cedana restore runc -e -b "$bundle" --dir "$dir" --id "$id" --console-socket "$tty"
}

function fail() {
  echo "$@" >&2
  exit 1
}
