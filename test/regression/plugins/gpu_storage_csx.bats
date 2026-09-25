#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=gpu,storage:csx

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/csx
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_LOG_LEVEL=trace

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    setup_file_csx_daemon
    setup_file_daemon
}

setup() {
    setup_csx_daemon
    setup_daemon
}

teardown() {
    teardown_daemon
    teardown_csx_daemon
}

teardown_file() {
    teardown_file_daemon
    teardown_file_csx_daemon
}

############
### Dump ###
############

# bats test_tags=dump
@test "[$GPU_INFO] (CSX) dump GPU process (vector add)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    watch_logs "$jid"

    sleep 1

    cedana dump job "$jid" --dir csx://

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "[$GPU_INFO] (CSX) dump GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    watch_logs "$jid"

    sleep 1

    cedana dump job "$jid" --dir csx://

    run cedana job kill "$jid"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "[$GPU_INFO] (CSX) restore GPU process (vector add)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    watch_logs "$jid"

    sleep 1

    cedana dump job "$jid" --dir csx://

    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
}

# bats test_tags=restore
@test "[$GPU_INFO] (CSX) restore GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    watch_logs "$jid"

    sleep 1

    cedana dump job "$jid" --dir csx://

    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
}

# bats test_tags=restore,serverless
@test "[$GPU_INFO] (CSX) restore GPU process (mem throughput saxpy, without daemon)" {
    jid=$(unix_nano)
    pid_file=/tmp/pid-$jid

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    watch_logs "$jid"

    sleep 1

    run cedana dump job "$jid" --dir csx://
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')

    debug cedana restore process --path "$dump_file" --pid-file "$pid_file" --no-server &

    wait_for_file "$pid_file"
    pid=$(cat "$pid_file")
    kill -KILL "$pid"
    wait_for_no_pid "$pid"
}
