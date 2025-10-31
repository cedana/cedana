#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu,streamer,storage:cedana

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

############
### Dump ###
############

# bats test_tags=dump
@test "[$GPU_INFO] remote stream dump GPU process (vector add)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add

    sleep 1

    cedana dump job "$jid" --streams 1 --dir cedana://ci

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "[$GPU_INFO] remote stream dump GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop

    sleep 1

    cedana dump job "$jid" --streams 4 --dir cedana://ci

    run cedana job kill "$jid"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "[$GPU_INFO] remote stream restore GPU process (vector add)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add

    sleep 1

    cedana dump job "$jid" --streams 1 --dir cedana://ci

    cedana restore job "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
}

# bats test_tags=restore
@test "[$GPU_INFO] remote stream restore GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop

    sleep 1

    cedana dump job "$jid" --streams 4 --dir cedana://ci

    cedana restore job "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
}

# bats test_tags=restore,daemonless
@test "[$GPU_INFO] remote stream restore GPU process (mem throughput saxpy, without daemon)" {
    jid=$(unix_nano)
    pid_file=/tmp/pid-$jid

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop

    sleep 1

    run cedana dump job "$jid" --streams 4 --dir cedana://ci
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')

    debug cedana restore process --path "$dump_file" --pid-file "$pid_file" --no-server &

    wait_for_file "$pid_file"
    pid=$(cat "$pid_file")
    kill -KILL "$pid"
    wait_for_no_pid "$pid"
}
