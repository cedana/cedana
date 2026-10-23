#!/usr/bin/env bats

# A process that destroys cuFFT plans and creates new ones before a checkpoint, as the PME
# tuning of GROMACS does. cuFFT unloads the modules of a destroyed plan. Skips if the samples
# image predates the binary.
#
# bats file_tags=gpu,cufft

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget

CUFFT_REPLAN=/cedana-samples/gpu_smr/cufft_replan

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    [ -x "$CUFFT_REPLAN" ] || skip "cufft_replan not in image (rebuild cedana-samples image)"
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

# Runs the sample with a gate file, dumps it after READY, restores it, opens the gate and
# checks that every FFT round trip after the restore is right. Args: extra sample flags.
dump_restore_cufft() {
    jid=$(unix_nano)
    gate=/tmp/gate-$jid
    rm -f "$gate"

    cedana run process -g --jid "$jid" -- "$CUFFT_REPLAN" "$@" "$gate"
    log_file=$(logfile_for_jid "$jid")
    watch_logs "$jid"

    wait_for_cmd 60 grep -q READY "$log_file" # the last plan pair exists and works

    cedana dump job "$jid"

    cedana restore job "$jid"
    log_file=$(logfile_for_jid "$jid")
    watch_logs "$jid"

    sleep 1
    touch "$gate" # finish the round trips on the restored plans

    wait_for_cmd 60 grep -q PASS "$log_file"
    run grep -E "MISMATCH|FAIL" "$log_file"
    assert_failure

    rm -f "$gate"
    run cedana job kill "$jid"
}

###########
### Run ###
###########

@test "[$GPU_INFO] run GPU process (cuFFT plans destroyed and created again)" {
    jid=$(unix_nano)

    run cedana run process --attach -g --jid "$jid" -- "$CUFFT_REPLAN"
    assert_success
    assert_output --partial "PASS"
}

@test "[$GPU_INFO] run GPU process (one cuFFT plan pair)" {
    jid=$(unix_nano)

    run cedana run process --attach -g --jid "$jid" -- "$CUFFT_REPLAN" --control
    assert_success
    assert_output --partial "PASS"
}

###############
### Restore ###
###############

# The control: no plan is destroyed, so no module is unloaded before the dump.
# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (one cuFFT plan pair)" {
    dump_restore_cufft --control
}

# The dump drops the load record of each module that cufftDestroy unloaded, and the GPU
# worker must also drop the hiddenFFTModule record of that module. With the record kept, the
# replay gives libcuda a module handle of the first worker, and the worker segfaults.
# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuFFT plans destroyed and created again)" {
    skip "the GPU worker replays the cuFFT module call of an unloaded module (CED-2542); remove after a cedana-gpu release with the fix"

    dump_restore_cufft
}
