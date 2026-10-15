#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

pull_images() {
    if ! cmd_exists podman; then
        error_log "podman is not installed. Please install it first."
        exit 1
    fi

    podman pull docker.io/library/alpine:latest
    podman pull docker.io/library/nginx:latest
    # Add more images as needed
}

export IMAGE_CUDA="localhost/cedana-samples-cuda:test"

# Builds a small image with the CUDA samples used in tests, from /cedana-samples.
# Smaller than the full samples image, as the vfs storage driver used in tests
# copies the entire image for every container.
#
# NOTE: CDI only mounts the versioned driver libraries, and its createContainer
# hook creates their symlinks (e.g. libcuda.so -> libcuda.so.1 -> libcuda.so.<version>),
# which the GPU worker needs. These hooks aren't re-run on restore, and the vfs
# storage driver can't preserve the symlinks in the read/write layer, so bake
# them into the image.
build_image_cuda() {
    local dir libdir
    dir=$(mktemp -d)
    cp /cedana-samples/gpu_smr/mem-throughput-saxpy /cedana-samples/gpu_smr/mem-throughput-saxpy-loop "$dir"
    grep -oE '^\s*- [^ ]+::/usr/[^ ]+$' /etc/cdi/nvidia.yaml | sed 's/^\s*- //' | sort -u |
        awk -F'::' '{printf "mkdir -p $(dirname %s) && ln -sf %s %s\n", $2, $1, $2}' > "$dir/links.sh"
    libdir=$(dirname "$(grep -oE '::/usr/[^ ]*/libcuda\.so$' /etc/cdi/nvidia.yaml | head -1 | sed 's/^:://')")
    printf '%s\n' \
        'FROM docker.io/library/ubuntu:22.04' \
        'COPY mem-throughput-saxpy mem-throughput-saxpy-loop links.sh /' \
        'RUN sh /links.sh' \
        "ENV LD_LIBRARY_PATH=$libdir" > "$dir/Containerfile"
    podman build --quiet --tag "$IMAGE_CUDA" "$dir"
    rm -rf "$dir"
}

# Waits for a podman container to be running
wait_for_container() {
    local name="$1"
    wait_for_cmd 30 "[ \"\$(podman inspect --format '{{.State.Status}}' $name)\" = running ]"
}

container_status() {
    local name="$1"
    podman inspect --format '{{.State.Status}}' "$name"
}
