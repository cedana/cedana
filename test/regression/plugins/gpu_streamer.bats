#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu,streamer

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
@test "stream dump GPU process (vector add)" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    assert_success
    assert_exists "$log_file"

    sleep 2

    run cedana dump job "$jid" --stream 1
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0.gz"

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "stream dump GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    assert_success
    assert_exists "$log_file"

    sleep 2

    run cedana dump job "$jid" --stream 4
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0.gz"
    assert_exists "$dump_file/img-1.gz"
    assert_exists "$dump_file/img-2.gz"
    assert_exists "$dump_file/img-3.gz"

    run cedana job kill "$jid"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "stream restore GPU process (vector add)" {
    jid=$(unix_nano)

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    assert_success

    sleep 2

    run cedana dump job "$jid" --stream 1
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0.gz"

    run cedana restore job "$jid" --stream 1
    assert_success

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
}

# bats test_tags=restore
@test "stream restore GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    assert_success

    sleep 2

    run cedana dump job "$jid" --stream 4
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0.gz"
    assert_exists "$dump_file/img-1.gz"
    assert_exists "$dump_file/img-2.gz"
    assert_exists "$dump_file/img-3.gz"

    run cedana restore job "$jid" --stream 4
    assert_success

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
}

