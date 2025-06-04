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

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget
export CEDANA_GPU_SHM_SIZE=$((1*GIBIBYTE)) # Since workloads here are small

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    setup_file_daemon
    do_once setup_rootfs_cuda
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
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"
    bundle="$(create_cmd_bundle_cuda "echo hello")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled

    assert_success
    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"

    run cedana ps

    assert_success
    assert_output --partial "$jid"
}

@test "run GPU container (GPU binary)" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/mem-throughput-saxpy")"

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
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/vector_add")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled
    assert_success

    sleep 1

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "dump GPU container (mem throughput saxpy)" {
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/mem-throughput-saxpy-loop")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled
    assert_success

    sleep 1

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "restore GPU container (vector add)" {
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/vector_add")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled
    assert_success

    sleep 1

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore job "$jid"
    assert_success

    run cedana job kill "$jid"
    run cedana job delete "$jid"
}

# bats test_tags=restore
@test "restore GPU container (mem throughput saxpy)" {
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/mem-throughput-saxpy-loop")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled
    assert_success

    sleep 1

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore job "$jid"
    assert_success

    run cedana job kill "$jid"
    run cedana job delete "$jid"
}
