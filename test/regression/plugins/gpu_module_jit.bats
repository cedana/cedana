#!/usr/bin/env bats

# cuModuleLoadDataEx option marshalling and restored module/function handles.
# The workload checks CUDA results and launches a kernel from every live module.
# bats file_tags=gpu,module-jit

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

MODULE_JIT=/usr/local/libexec/cedana-tests/module-jit
export CEDANA_CHECKPOINT_COMPRESSION=gzip

setup_file() {
    cmd_exists nvidia-smi || skip "GPU not available"
    [ -x "$MODULE_JIT" ] || fail "module JIT workload missing (rebuild CUDA test image)"
    setup_file_daemon
}

setup() {
    setup_daemon
}

teardown() {
    if [ -n "${jid:-}" ]; then
        cedana job kill "$jid" >/dev/null 2>&1 || true
    fi
    if [ -n "${restored_pid:-}" ] && kill -0 "$restored_pid" 2>/dev/null; then
        kill "$restored_pid" 2>/dev/null || true
    fi
    teardown_daemon
}

teardown_file() {
    teardown_file_daemon
}

@test "[$GPU_INFO] module JIT options (native)" {
    run "$MODULE_JIT"
    assert_success
    assert_output --partial "PASS module JIT regression: result=42"
}

@test "[$GPU_INFO] module JIT options (intercepted)" {
    jid=$(unix_nano)
    run cedana run process --attach -g --jid "$jid" -- "$MODULE_JIT"
    assert_success
    assert_output --partial "PASS module JIT regression: result=42"
}

# bats test_tags=restore
@test "[$GPU_INFO] module JIT options and kernels after restore" {
    jid=$(unix_nano)
    local artifacts="$BATS_TEST_TMPDIR" gate="$BATS_TEST_TMPDIR/launch"
    debug cedana run process -g --jid "$jid" --out "$artifacts/original.log" -- \
        "$MODULE_JIT" "$gate"
    debug wait_for_cmd 60 grep -Fxq READY "$artifacts/original.log"
    debug cedana dump job --leave-running --link-remap --dir "$artifacts" \
        --name checkpoint "$jid"
    touch "$gate"
    debug wait_for_cmd 60 grep -Fxq 'PASS module JIT regression: result=42' "$artifacts/original.log"
    debug cedana restore process --path "$artifacts/checkpoint.tar.gz" --out "$artifacts/restored.log" \
        --pid-file "$artifacts/restored.pid"
    restored_pid=$(cat "$artifacts/restored.pid")
    debug wait_for_cmd 60 grep -Fxq 'PASS module JIT regression: result=42' "$artifacts/restored.log"
}
