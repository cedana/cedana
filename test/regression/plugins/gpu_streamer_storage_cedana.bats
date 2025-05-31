#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu,streamer,storage:cedana

load ../helpers/utils
load ../helpers/daemon
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

############
### Dump ###
############

# bats test_tags=dump
@test "remote stream dump GPU process (vector add)" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    assert_success
    assert_exists "$log_file"

    sleep 2

    run cedana dump job "$jid" --stream 1 --dir cedana://ci
    assert_success

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "remote stream dump GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    assert_success
    assert_exists "$log_file"

    sleep 2

    run cedana dump job "$jid" --stream 4 --dir cedana://ci
    assert_success

    run cedana job kill "$jid"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "remote stream restore GPU process (vector add)" {
    jid=$(unix_nano)

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    assert_success

    sleep 2

    run cedana dump job "$jid" --stream 1 --dir cedana://ci
    assert_success

    run cedana restore job "$jid" --stream 1
    assert_success

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
}

# bats test_tags=restore
@test "remote stream restore GPU process with smaller shm (vector add)" {
    jid=$(unix_nano)

    expected_size=$((4*1024*1024*1024))
    export CEDANA_GPU_SHM_SIZE="$expected_size"

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    assert_success

    # NOTE: GPU controller no longer uses JID, so below is commented out
    # check_shm_size "$jid" "$expected_size"

    sleep 2

    run cedana dump job "$jid" --stream 1 --dir cedana://ci
    assert_success

    run cedana restore job "$jid" --stream 1
    assert_success

    # NOTE: GPU controller no longer uses JID, so below is commented out
    # check_shm_size "$jid" "$expected_size"

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
}

# bats test_tags=restore
@test "remote stream restore GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    assert_success

    sleep 2

    run cedana dump job "$jid" --stream 4 --dir cedana://ci
    assert_success

    run cedana restore job "$jid" --stream 4
    assert_success

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
}

