#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# CUDA API patterns real applications use, which crash or fail under the interceptor, or
# after a restore, when it doesn't handle them. Each sample checks its own results, and
# prints PASS and exits 0 only if they're right.
#
# bats file_tags=gpu,gpuapi

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget

SAMPLES=/cedana-samples/gpu_smr

setup_file() {
    trap_gpu_signals
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    setup_file_daemon
}

setup() {
    trap_gpu_signals
    setup_daemon
}

teardown() {
    teardown_daemon
    check_gpu_signals
}

teardown_file() {
    teardown_file_daemon
    check_gpu_signals
}

require_sample() {
    if [ ! -x "$SAMPLES/$1" ]; then
        skip "sample $1 is not in this image (needs cedana-samples v0.4.3+)"
    fi
}

# A device check at startup that resets the device, then work, as GROMACS does
# bats test_tags=run,reset
@test "[$GPU_INFO] run GPU process using the device after cudaDeviceReset" {
    require_sample reset_stream

    run cedana run process --attach -g --jid "$(unix_nano)" -- "$SAMPLES"/reset_stream
    assert_success
    assert_output --partial PASS
}

# The runtime's destructors run after a reset at exit
# bats test_tags=run,reset
@test "[$GPU_INFO] run GPU process resetting the device before it exits" {
    require_sample reset_exit

    run cedana run process --attach -g --jid "$(unix_nano)" -- "$SAMPLES"/reset_exit
    assert_success
    assert_output --partial PASS
}

# Reset at startup, checkpointed mid-work, restored, then reset again before it exits
# bats test_tags=restore,reset
@test "[$GPU_INFO] restore GPU process that resets the device before and after the checkpoint" {
    require_sample reset_exit

    local jid gate
    jid=$(unix_nano)
    gate="/tmp/cedana-gate-$jid"

    cedana run process -g --jid "$jid" -- "$SAMPLES"/reset_exit "$gate"
    local log
    log=$(logfile_for_jid "$jid")
    wait_for_cmd 60 grep -q READY "$log"

    cedana dump job "$jid"

    # Lets the restored process finish its work
    touch "$gate"
    run cedana restore job "$jid" --attach
    rm -f "$gate"
    assert_success
    assert_output --partial PASS
}
