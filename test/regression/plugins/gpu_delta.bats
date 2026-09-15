#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu,delta

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

# Delta chains restore by walking parent image dirs on disk, so images must
# stay un-tarred. CEDANA_GPU_DELTA_ENABLED is deliberately left unset: these
# tests exercise the per-request --incremental flag.
export CEDANA_CHECKPOINT_COMPRESSION=none

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
@test "[$GPU_INFO] dump GPU process with per-request incremental (vector add)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    watch_logs "$jid"

    sleep 1

    # Base checkpoint (full), then an explicitly incremental one chained onto it
    cedana dump job "$jid"

    sleep 1

    cedana dump job "$jid" --incremental

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process from incremental chain (vector add)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    watch_logs "$jid"

    sleep 1

    cedana dump job "$jid"

    sleep 1

    cedana dump job "$jid" --incremental

    # Restores the latest (delta) checkpoint, which walks back to the base
    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}
