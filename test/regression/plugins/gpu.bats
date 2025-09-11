#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu

load ../../helpers/utils
load ../../helpers/daemon
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
    jid=$(unix_nano)

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy --attach
    assert_success
}

# bats test_tags=daemonless
@test "run GPU process (GPU binary, without daemon)" {
    jid=$(unix_nano)

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy --no-server
    assert_success
}

@test "run GPU process (non-existent binary)" {
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
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    assert_success
    assert_exists "$log_file"

    sleep 2

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=dump
@test "dump GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    assert_success
    assert_exists "$log_file"

    sleep 2

    run cedana dump job "$jid"
    assert_success

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
    jid=$(unix_nano)

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    assert_success

    sleep 2

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore job "$jid"
    assert_success

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=restore
@test "restore GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    assert_success

    sleep 2

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore job "$jid"
    assert_success

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=restore,daemonless
@test "restore GPU process (mem throughput saxpy, without daemon)" {
    jid=$(unix_nano)

    run cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    assert_success

    pid=$(pid_for_jid "$jid")

    sleep 2

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore process --path "$dump_file" --no-server &

    wait_for_pid "$pid"
    run kill -KILL "$pid"
}
