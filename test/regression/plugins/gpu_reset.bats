#!/usr/bin/env bats

# Processes that call cudaDeviceReset() and then use the device, as GROMACS does at startup and
# again before exit. Skips if the samples image predates the binaries.
#
# bats file_tags=gpu,reset

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget

RESET_STREAM=/cedana-samples/gpu_smr/reset_stream
RESET_EXIT=/cedana-samples/gpu_smr/reset_exit

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    for bin in "$RESET_STREAM" "$RESET_EXIT"; do
        [ -x "$bin" ] || skip "reset samples not in image (rebuild cedana-samples image)"
    done
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

# A stream after the reset fails with cudaErrorContextIsDestroyed (709) when the interceptor
# keeps the runtime's per-context state through the reset.
@test "[$GPU_INFO] run GPU process (stream after cudaDeviceReset)" {
    jid=$(unix_nano)

    run cedana run process --attach -g --jid "$jid" -- "$RESET_STREAM"
    assert_success
    assert_output --partial "PASS"
}

# The second reset is just before exit; the runtime's global destructor runs after it, and
# crashes the process when the runtime's context-local storage destructors did not run.
@test "[$GPU_INFO] run GPU process (cudaDeviceReset at startup and before exit)" {
    jid=$(unix_nano)

    run cedana run process --attach -g --jid "$jid" -- "$RESET_EXIT"
    assert_success
    assert_output --partial "PASS"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cudaDeviceReset at startup and before exit)" {
    # The worker drops the record of a library load with the context that a reset destroys,
    # although a cuLibrary outlives the reset; the replay of the runtime's later
    # cuLibraryGetModule on that library then waits until the restore times out.
    skip "restore after a reset hangs in the replay of cuLibraryGetModule (CED-2507)"

    jid=$(unix_nano)
    gate=/tmp/gate-$jid
    rm -f "$gate"

    cedana run process -g --jid "$jid" -- "$RESET_EXIT" "$gate"
    log_file=$(logfile_for_jid "$jid")
    watch_logs "$jid"

    wait_for_cmd 30 grep -q READY "$log_file" # after the startup reset, iterating

    cedana dump job "$jid"

    cedana restore job "$jid"
    log_file=$(logfile_for_jid "$jid")
    watch_logs "$jid"

    sleep 1
    touch "$gate" # finish the work, then the reset before exit

    wait_for_cmd 60 grep -q PASS "$log_file"
    run grep -E "MISMATCH|FAIL" "$log_file"
    assert_failure

    rm -f "$gate"
    run cedana job kill "$jid"
}
