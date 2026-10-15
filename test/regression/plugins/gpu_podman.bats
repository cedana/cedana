#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu,podman

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/podman
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    setup_file_daemon
    do_once build_image_cuda
}

setup() {
    setup_daemon
}

teardown() {
    teardown_daemon
}

teardown_file() {
    teardown_file_daemon
}

###########
### Run ###
###########

# bats test_tags=attach
@test "[$GPU_INFO] run GPU container (podman)" {
    jid=$(unix_nano)

    run cedana run podman --jid "$jid" --gpu-enabled --attach -- "$IMAGE_CUDA" /mem-throughput-saxpy
    assert_success
}

############
### Dump ###
############

# bats test_tags=dump
@test "[$GPU_INFO] dump GPU container (podman)" {
    jid=$(unix_nano)

    cedana run podman --jid "$jid" --gpu-enabled -- "$IMAGE_CUDA" /mem-throughput-saxpy-loop

    sleep 2

    cedana dump job "$jid"

    run cedana job kill "$jid"
}

# Containers run with cedana-oci-runtime directly get GPU interception automatically
# bats test_tags=dump
@test "[$GPU_INFO] dump GPU container (podman, cedana runtime)" {
    id=$(unix_nano)

    podman run --runtime "$(command -v cedana-oci-runtime)" --env NVIDIA_VISIBLE_DEVICES=all --detach --name "$id" "$IMAGE_CUDA" /mem-throughput-saxpy-loop

    sleep 2

    run cedana dump podman "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run podman rm --force --time 0 "$id"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU container (podman)" {
    jid=$(unix_nano)

    cedana run podman --jid "$jid" --gpu-enabled -- "$IMAGE_CUDA" /mem-throughput-saxpy-loop

    sleep 2

    cedana dump job "$jid"

    cedana restore job "$jid"

    wait_for_container "$jid"

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
    run cedana job delete "$jid"
}

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU container (podman, cedana runtime)" {
    id=$(unix_nano)

    podman run --runtime "$(command -v cedana-oci-runtime)" --env NVIDIA_VISIBLE_DEVICES=all --detach --name "$id" "$IMAGE_CUDA" /mem-throughput-saxpy-loop

    sleep 2

    run cedana dump podman "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"
    podman rm --force --time 0 "$id"

    cedana restore podman --path "$dump_file" --id "$id"

    wait_for_container "$id"

    run podman rm --force --time 0 "$id"
}
