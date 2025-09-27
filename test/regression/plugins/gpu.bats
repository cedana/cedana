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

    cedana run process -g --jid "$jid" -- echo hello

    assert_exists "$log_file"

    run cedana ps
    assert_success
    assert_output --partial "$jid"
}

@test "run GPU process (GPU binary)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy --attach
}

# bats test_tags=daemonless
@test "run GPU process (GPU binary, without daemon)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy --no-server
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

    cedana exec -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy

    assert_exists "$log_file"
}

############
### Dump ###
############

# bats test_tags=dump
@test "dump GPU process (non-GPU binary)" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    cedana run process -g --jid "$jid" -- "$WORKLOADS"/date-loop.sh

    assert_exists "$log_file"

    sleep 1

    cedana dump job "$jid"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=dump
@test "dump GPU process (vector add)" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add

    assert_exists "$log_file"

    sleep 1

    cedana dump job "$jid"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=dump
@test "dump GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop

    assert_exists "$log_file"

    sleep 1

    cedana dump job "$jid"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "restore GPU process (non-GPU binary)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- "$WORKLOADS"/date-loop.sh

    sleep 1

    cedana dump job "$jid"

    cedana restore job "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=restore
@test "restore GPU process (vector add)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add

    sleep 1

    cedana dump job "$jid"

    cedana restore job "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=restore
@test "restore GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop

    sleep 1

    cedana dump job "$jid"

    cedana restore job "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=restore,daemonless
@test "restore GPU process (mem throughput saxpy, without daemon)" {
    jid=$(unix_nano)
    pid_file=$(mktemp)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop

    sleep 1

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    debug cedana restore process --path "$dump_file" --pid-file "$pid_file" --no-server &

    sleep 5

    wait_for_file "$pid_file"
    pid=$(cat "$pid_file")
    run kill -KILL "$pid"
    wait_for_no_pid "$pid"
}
