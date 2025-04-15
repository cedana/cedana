#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu,runc

load ../helpers/utils
load ../helpers/daemon
load ../helpers/runc
load ../helpers/gpu

load_lib support
load_lib assert
load_lib file

# One-time setup of downloading weights & pip installing
setup_file() {
    setup_file_daemon
    if cmd_exists nvidia-smi; then
        do_once install_requirements
        do_once download_hf_models
    fi
    do_once setup_rootfs
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

@test "run GPU container (non-GPU binary)" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"
    bundle="$(create_cmd_bundle "echo hello")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled

    assert_success
    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"

    run cedana ps

    assert_success
    assert_output --partial "$jid"
}

@test "run GPU container (GPU binary)" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"
    bundle="$(create_samples_workload_bundle "gpu_smr/mem-throughput-saxpy")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled

    assert_success
    assert_exists "$log_file"

    run cedana ps

    assert_success
    assert_output --partial "$jid"
}

############
### Dump ###
############

# bats test_tags=dump
@test "dump GPU container (vector add)" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle "gpu_smr/vector_add")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled
    assert_success

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"
    assert_success
}

###############
### Restore ###
###############
