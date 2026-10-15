#!/usr/bin/env bats

# GPU freeze of a job whose default stream holds seconds of queued work. The
# freeze parks the job's threads, then waits for the GPU to finish what is
# already queued. deep_queue keeps about 2 s of kernels queued, more than the
# controller's freeze windows of 150 ms and 300 ms, as a GPU-bound job such as
# ESMFold does. A single-device job gets a longer drain, so the dump succeeds.
# deep_queue self-validates its completions and exits non-zero on a gap, so a
# corrupted restore halts the job.
#
# Requires deep_queue from the cedana-samples image, and a cedana-gpu with the
# single-device drain; skips if the image predates the sample.
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

    cedana dump job "$jid"

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

    cedana dump job "$jid"

    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 3

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}
