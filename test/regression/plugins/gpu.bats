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

@test "[$GPU_INFO] run GPU process (non-GPU binary)" {
    jid=$(unix_nano)

    debug cedana run process --attach -g --jid "$jid" -- echo hello
}

@test "[$GPU_INFO] run GPU process (GPU binary)" {
    jid=$(unix_nano)

    debug cedana run process --attach -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy
}

# bats test_tags=daemonless
@test "[$GPU_INFO] run GPU process (GPU binary, without daemon)" {
    jid=$(unix_nano)

    debug cedana run process --no-server -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy
}

@test "[$GPU_INFO] run GPU process (non-existent binary)" {
    jid=$(unix_nano)

    run cedana exec -g --jid "$jid" -- non-existent
    assert_failure
}

@test "[$GPU_INFO] exec GPU process (run process alias)" {
    jid=$(unix_nano)

    debug cedana exec --attach -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy
}

############
### Dump ###
############

# bats test_tags=dump
@test "[$GPU_INFO] dump GPU process (non-GPU binary)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- "$WORKLOADS"/date-loop.sh
    watch_logs "$jid"

    sleep 1

    cedana dump job "$jid"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=dump
@test "[$GPU_INFO] dump GPU process (vector add)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    watch_logs "$jid"

    sleep 1

    cedana dump job "$jid"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=dump
@test "[$GPU_INFO] dump GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    watch_logs "$jid"

    sleep 1

    cedana dump job "$jid"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (non-GPU binary)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- "$WORKLOADS"/date-loop.sh
    watch_logs "$jid"

    sleep 1

    cedana dump job "$jid"

    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (vector add)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    watch_logs "$jid"

    sleep 1

    cedana dump job "$jid"

    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    watch_logs "$jid"

    sleep 1

    cedana dump job "$jid"

    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

# bats test_tags=restore,daemonless
@test "[$GPU_INFO] restore GPU process (mem throughput saxpy, without daemon)" {
    jid=$(unix_nano)
    pid_file=/tmp/pid-$jid

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop
    watch_logs "$jid"

    sleep 1

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    debug cedana restore process --path "$dump_file" --pid-file "$pid_file" --no-server &

    wait_for_file "$pid_file"
    pid=$(cat "$pid_file")
    kill -KILL "$pid"
    wait_for_no_pid "$pid"
}
