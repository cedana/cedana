#!/usr/bin/env bats

# bats file_tags=gpu,global-pointer-replay

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu
load_lib support
load_lib assert

export CEDANA_CHECKPOINT_COMPRESSION=gzip
ARENA_PLACEMENT=/usr/local/libexec/cedana-tests/gpu-arena-placement.so
GLOBAL_REPLAY=/usr/local/libexec/cedana-tests/module-global-replay

setup_file() {
    cmd_exists nvidia-smi || skip "GPU not available"
    [ -x "$GLOBAL_REPLAY" ] || skip "global-pointer fixture not in image (rebuild cedana-test:cuda)"
    [ -r "$ARENA_PLACEMENT" ] || skip "arena audit library not in image (rebuild cedana-test:cuda)"
    export LD_AUDIT="$ARENA_PLACEMENT"
    export CEDANA_TEST_OLD_GLOBAL_FILE="/tmp/global-pointer-$(unix_nano)-old-address"
    setup_file_daemon
}

setup() {
    jid="$(unix_nano)-global-pointer"
    gate="$BATS_TEST_TMPDIR/$jid-resume"
    output_file="$BATS_TEST_TMPDIR/$jid.log"
    rm -f "$CEDANA_TEST_OLD_GLOBAL_FILE"
    setup_daemon
}

teardown() {
    # Stop any workload still waiting at the gate after a failed assertion.
    cedana job kill "$jid" >/dev/null 2>&1 || true
    teardown_daemon
}

teardown_file() {
    teardown_file_daemon
    rm -f "$CEDANA_TEST_OLD_GLOBAL_FILE"
}

restore_global_pointer() {
    run cedana run process -g --jid "$jid" --out "$output_file" -- "$GLOBAL_REPLAY" "$gate" "$@"
    assert_success
    wait_for_cmd 60 test -s "$gate.ready"
    run cedana dump job "$jid"
    assert_success
    if [[ "${1:-}" == allocation ]]; then
        cp "$gate.ready" "$CEDANA_TEST_OLD_GLOBAL_FILE"
    fi
    run cedana restore job "$jid"
    assert_success
    touch "$gate"
    wait_for_cmd 60 grep -qxE 'DONE|FAIL .*' "$output_file" || {
        cat "$output_file" >&3
        fail "Restored workload did not complete"
    }
    run cat "$output_file"
    assert_output --partial "RESULT 42"
    assert_output --partial "DONE"
    refute_output --partial "FAIL"
}

# bats test_tags=restore
@test "[$GPU_INFO] restore module global then HtoDAsync into its middle" {
    restore_global_pointer
}

# bats test_tags=restore
@test "[$GPU_INFO] post-restore allocation overlapping old module global range" {
    restore_global_pointer allocation
    assert_output --partial "OVERLAP old="
}
