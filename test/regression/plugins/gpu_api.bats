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

# XXX: remove once cedana-gpu #309 (CED-2507) is in the GPU plugin; these hang until then
require_reset_fix() {
    skip "known bug: CUDA applications that reset the device hang under the interceptor (cedana-gpu #309)"
}

# A device check at startup that resets the device, then work, as GROMACS does
# bats test_tags=run,reset
@test "[$GPU_INFO] run GPU process using the device after cudaDeviceReset" {
    require_sample reset_stream
    require_reset_fix

    run timeout 180 cedana run process --attach -g --jid "$(unix_nano)" -- "$SAMPLES"/reset_stream
    assert_success
    assert_output --partial PASS
}

# The runtime's destructors run after a reset at exit
# bats test_tags=run,reset
@test "[$GPU_INFO] run GPU process resetting the device before it exits" {
    require_sample reset_exit
    require_reset_fix

    run timeout 180 cedana run process --attach -g --jid "$(unix_nano)" -- "$SAMPLES"/reset_exit
    assert_success
    assert_output --partial PASS
}

# Reset at startup, checkpointed mid-work, restored, then reset again before it exits
# bats test_tags=restore,reset
@test "[$GPU_INFO] restore GPU process that resets the device before and after the checkpoint" {
    require_sample reset_exit
    require_reset_fix

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
    run timeout 180 cedana restore job "$jid" --attach
    rm -f "$gate"
    assert_success
    assert_output --partial PASS
}

# A main process that isn't CUDA, which runs a short-lived CUDA probe at startup (e.g. from
# BASH_ENV): the probe exiting must not take the job down, and the job must still checkpoint
# and restore (customer issue P-17)
# bats test_tags=restore,probe
@test "[$GPU_INFO] restore GPU job after a short-lived CUDA probe in it" {
    require_sample gpu_props
    # XXX: remove once fixed, this test found it
    skip "known bug: when the job's only CUDA process exits, the GPU controller shuts down and signals the job's whole process group (SIGUSR2, exit 12)"

    local jid log
    jid=$(unix_nano)
    cedana run process -g --jid "$jid" -- sh -c "$SAMPLES/gpu_props >/dev/null && echo PROBED; while true; do echo alive; sleep 0.5; done"
    log=$(logfile_for_jid "$jid")
    wait_for_cmd 60 grep -q PROBED "$log"
    sleep 2

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    cedana dump job "$jid"
    local lines
    lines=$(wc -l <"$log")
    cedana restore job "$jid"
    wait_for_cmd 30 "tail -n +$((lines + 1)) $log | grep -q alive"

    run cedana job kill "$jid"
}

# A CUDA process started after the restore, by a restored process that isn't CUDA itself
# (customer issue P-34)
# bats test_tags=restore,child
@test "[$GPU_INFO] restore GPU job, then start a CUDA child process in it" {
    require_sample gpu_props
    # XXX: remove once fixed, this test found it
    skip "known bug: a CUDA process started after the restore hangs, as does the controller's restore (it never completes once the process attaches)"

    local jid gate
    jid=$(unix_nano)
    gate="/tmp/cedana-gate-$jid"
    cedana run process -g --jid "$jid" -- sh -c "while [ ! -f $gate ]; do sleep 0.2; done; $SAMPLES/gpu_props; s=\$?; echo CHILD_EXIT=\$s; exit \$s"
    sleep 1

    cedana dump job "$jid"

    touch "$gate"
    run timeout 180 cedana restore job "$jid" --attach
    rm -f "$gate"
    assert_success
    assert_output --partial "Device name"
    assert_output --partial "CHILD_EXIT=0"
}

# Pinned host memory (cudaHostRegister, from malloc and MAP_SHARED) that kernels use zero-copy
# (customer issues P-14, P-43)
# bats test_tags=run,hostregister
@test "[$GPU_INFO] run GPU process using registered host memory zero-copy" {
    require_sample host_register

    run timeout 180 cedana run process --attach -g --jid "$(unix_nano)" -- "$SAMPLES"/host_register
    assert_success
    assert_output --partial PASS
}

# bats test_tags=restore,hostregister
@test "[$GPU_INFO] restore GPU process using registered host memory zero-copy" {
    require_sample host_register

    local jid gate log
    jid=$(unix_nano)
    gate="/tmp/cedana-gate-$jid"

    cedana run process -g --jid "$jid" -- "$SAMPLES"/host_register "$gate"
    log=$(logfile_for_jid "$jid")
    wait_for_cmd 60 grep -q READY "$log"

    cedana dump job "$jid"

    touch "$gate"
    run timeout 180 cedana restore job "$jid" --attach
    rm -f "$gate"
    assert_success
    assert_output --partial PASS
}
