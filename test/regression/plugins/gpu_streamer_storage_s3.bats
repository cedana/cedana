#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu,streamer,storage:s3

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
@test "remote (S3) stream dump GPU process (vector add)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add

    sleep 1

    cedana dump job "$jid" --streams 1 --dir s3://checkpoints-ci

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "remote (S3) stream dump GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop

    sleep 1

    cedana dump job "$jid" --streams 4 --dir s3://checkpoints-ci

    run cedana job kill "$jid"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "remote (S3) stream restore GPU process (vector add)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add

    sleep 1

    cedana dump job "$jid" --streams 1 --dir s3://checkpoints-ci

    cedana restore job "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
}

# bats test_tags=restore
@test "remote (S3) stream restore GPU process (mem throughput saxpy)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop

    sleep 1

    cedana dump job "$jid" --streams 4 --dir s3://checkpoints-ci

    cedana restore job "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
}

# bats test_tags=restore,daemonless
@test "remote (S3) stream restore GPU process (mem throughput saxpy, without daemon)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/mem-throughput-saxpy-loop

    pid=$(pid_for_jid "$jid")

    sleep 1

    run cedana dump job "$jid" --streams 4 --dir s3://checkpoints-ci
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')

    cedana restore process --path "$dump_file" --no-server &

    wait_for_pid "$pid"
    run kill -KILL "$pid"
    wait_for_no_pid "$pid"
}
