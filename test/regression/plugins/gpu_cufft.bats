#!/usr/bin/env bats

# cuFFT plans destroyed and created again before a checkpoint (CED-2542).
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
    # Kill the sample that a failed restore test left on the GPU.
    if [ -n "${gate:-}" ]; then
        rm -f "$gate"
        cedana job kill "$jid" &>/dev/null || true
        unset gate
    fi
    teardown_daemon
}

teardown_file() {
    teardown_file_daemon
}

# Dump after READY, restore, open the gate and check the result. Args: sample flags.
dump_restore_cufft() {
    jid=$(unix_nano)
    gate=/tmp/gate-$jid
    rm -f "$gate"

    cedana run process -g --jid "$jid" -- "$CUFFT_REPLAN" "$@" "$gate"
    log_file=$(logfile_for_jid "$jid")
    watch_logs "$jid"

    wait_for_cmd 60 grep -q READY "$log_file"

    cedana dump job "$jid"

    cedana restore job "$jid"
    log_file=$(logfile_for_jid "$jid")
    watch_logs "$jid"

    sleep 1
    touch "$gate"

    wait_for_cmd 60 grep -q PASS "$log_file"
    run grep -E "MISMATCH|FAIL" "$log_file"
    assert_failure
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

# Control: no plan is destroyed.
# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (one cuFFT plan pair)" {
    dump_restore_cufft --control
}

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuFFT plans destroyed and created again)" {
    skip "needs a cedana-gpu release with the CED-2542 fix"

    dump_restore_cufft
}
