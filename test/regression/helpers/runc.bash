#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

export WORKLOADS="test/workloads"
export ROOTFS_URL="https://dl-cdn.alpinelinux.org/alpine/v3.10/releases/x86_64/alpine-minirootfs-3.10.1-x86_64.tar.gz"

ROOTFS="/tmp/_rootfs"
BUNDLE="$WORKLOADS/bundle"

setup_rootfs() {
    # check if the rootfs is already downloaded
    if [ -d "$ROOTFS" ]; then
        return
    fi

    mkdir -p "$ROOTFS"

    wget -q -O /tmp/rootfs.tar.gz "$ROOTFS_URL"
    tar -C "$ROOTFS" -xzf /tmp/rootfs.tar.gz
    rm /tmp/rootfs.tar.gz
}

create_workload_bundle() {
    local workload="$1"
    local arg="$2"
    local workload_path="$WORKLOADS/$workload"

    if [ ! -f "$ROOTFS"/"$workload" ]; then
        cp "$workload_path" "$ROOTFS"
    fi

    bundle=$(mktemp -d)

    cp "$BUNDLE"/config.json "$bundle"

    local config="$bundle"/config.json
    if [ -n "$arg" ]; then
        args="[\"/$workload\",\"$arg\"]"
    else
        args="[\"/$workload\"]"
    fi

    # add args as an singleton array of strings
    jq ".process.args = $args" "$config" > "$config".tmp
    mv "$config".tmp "$config"

    echo "$bundle"
}

create_cmd_bundle() {
    local cmd="$1"
    local arg="$2"

    bundle=$(mktemp -d)

    cp "$BUNDLE"/config.json "$bundle"

    local config="$bundle"/config.json
    if [ -n "$arg" ]; then
        args="[\"/bin/sh\",\"-c\",\"$cmd\",\"$arg\"]"
    else
        args="[\"/bin/sh\",\"-c\",\"$cmd\"]"
    fi

    # add args as an singleton array of strings
    jq ".process.args = $args" "$config" > "$config".tmp
    mv "$config".tmp "$config"

    echo "$bundle"
}

# modifies a bundle to instead use an external namespace
share_namespace() {
    local bundle="$1"
    local type="$2"
    local path="$3"

    # remove item from namespaces array whose type is the type provided
    jq "del(.linux.namespaces[] | select(.type == \"$type\"))" "$bundle/config.json" > "$bundle/config.json.tmp"

    # add a new item to the namespaces array, with the same type as the one removed and a path to
    # the provided path
    jq ".linux.namespaces += [{\"type\":\"$type\",\"path\":\"$path\"}]" "$bundle/config.json.tmp" > "$bundle/config.json"
}

add_bind_mount() {
    local bundle="$1"
    local src="$2"
    local dest="$3"

    # add a new item to the mounts array, with the provided source and destination
    jq ".mounts += [{\"source\":\"$src\",\"destination\":\"$dest\",\"type\":\"bind\",\"options\":[\"rbind\",\"rw\"]}]" "$bundle/config.json" > "$bundle/config.json.tmp"
}

setup_rootfs
