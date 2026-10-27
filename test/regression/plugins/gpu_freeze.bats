#!/usr/bin/env bats

# Dump and restore of a job with about 2 s of work queued on the default stream
# (CED-2505). deep_queue exits non-zero on a gap in its count. Skips if the
# cedana-samples image has no deep_queue.
#
# bats file_tags=gpu,freeze

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget

DEEP_QUEUE=/cedana-samples/gpu_smr/deep_queue

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    [ -x "$DEEP_QUEUE" ] || skip "deep_queue sample not in image (rebuild cedana-samples image)"
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
@test "[$GPU_INFO] dump GPU process (deep queue on the default stream)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- "$DEEP_QUEUE"
    watch_logs "$jid"

    sleep 3

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (deep queue on the default stream)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- "$DEEP_QUEUE"
    watch_logs "$jid"

    sleep 3

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 3

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}
