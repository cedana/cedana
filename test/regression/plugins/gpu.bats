#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu

load ../helpers/utils
load ../helpers/daemon
load ../helpers/gpu

load_lib support
load_lib assert
load_lib file

# One-time setup of downloading weights & pip installing
setup_file() {
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

###########
### Run ###
###########

@test "run GPU process (non-GPU binary)" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana run process -g --jid "$jid" -- echo hello
    assert_success
    assert_exists "$log_file"

    run cedana ps
    assert_success
    assert_output --partial "$jid"
}

@test "run GPU process (GPU binary)" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy
    assert_success
    assert_exists "$log_file"
}

@test "run GPU process (GPU binary) with modified env" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    expected_size=$((4*1024*1024*1024))
    export CEDANA_GPU_SHM_SIZE="$expected_size"

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy
    assert_success
    assert_exists "$log_file"

    # NOTE: GPU controller no longer uses JID, so below is commented out
    # check_shm_size "$jid" "$expected_size"
}

@test "run GPU process (non-existent binary)" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana exec -g --jid "$jid" -- non-existent
    assert_failure
    assert_file_not_exist "$log_file"

    run cedana ps
    assert_success
    refute_output --partial "$jid"
}

@test "exec GPU process (run process alias)" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana exec -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy
    assert_success
    assert_exists "$log_file"
}

############
### Dump ###
############

# bats test_tags=dump
@test "dump GPU process (vector add)" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    assert_success
    assert_exists "$log_file"

    sleep 2

    run cedana dump job "$jid"
    assert_success

    sleep 1

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=dump
@test "dump GPU process (mem throughput saxpy)" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    assert_success
    assert_exists "$log_file"

    sleep 2

    run cedana dump job "$jid"
    assert_success

    sleep 1

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "restore GPU process (vector add)" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    assert_success

    sleep 2

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    sleep 3

    run cedana restore job "$jid"
    assert_success

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=restore
@test "restore GPU process with smaller shm (vector add)" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)

    expected_size=$((4*1024*1024*1024))
    export CEDANA_GPU_SHM_SIZE="$expected_size"

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    assert_success

    # NOTE: GPU controller no longer uses JID, so below is commented out
    # check_shm_size "$jid" "$expected_size"

    sleep 2

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    sleep 3

    run cedana restore job "$jid"
    assert_success

    # NOTE: GPU controller no longer uses JID, so below is commented out
    # check_shm_size "$jid" "$expected_size"

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=restore
@test "restore GPU process (mem throughput saxpy)" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    assert_success

    sleep 2

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    sleep 3

    run cedana restore job "$jid"
    assert_success

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}
