#!/usr/bin/env bats

# Run separately from the gpu suite so NCCL uses the GPU serially.
# bats file_tags=nccl

load ../../helpers/utils
load ../../helpers/daemon

load_lib support
load_lib assert

setup_file() {
    setup_file_daemon
}

setup() {
    setup_daemon
    jid="nccl-$(unix_nano)"
}

teardown() {
    timeout --kill-after=5s 15s cedana job kill "$jid" >/dev/null 2>&1 || true
    teardown_daemon
}

teardown_file() {
    teardown_file_daemon
}

check_collective() {
    local binary="/opt/nccl-tests/$1"
    local -a args=(-g "${2:-1}" -b 8 -e 1M -f 2 -n 5 -c 1 -T 30)

    run timeout --kill-after=5s 90s "$binary" "${args[@]}"
    assert_success
    assert_output --regexp '# Out of bounds values[[:space:]]*:[[:space:]]*0 OK'

    run timeout --kill-after=5s 90s cedana run process --attach --gpu-enabled \
        --jid "$jid" -- "$binary" "${args[@]}"
    assert_success
    # Check the workload's summary too: an attached CLI can return success
    # even when the workload fails.
    assert_output --regexp '# Out of bounds values[[:space:]]*:[[:space:]]*0 OK'
}

@test "NCCL single-GPU all-reduce (native and intercepted)" {
    check_collective all_reduce_perf
}

@test "NCCL single-GPU all-gather (native and intercepted)" {
    check_collective all_gather_perf
}

@test "NCCL single-GPU reduce-scatter (native and intercepted)" {
    check_collective reduce_scatter_perf
}

@test "NCCL single-GPU broadcast (native and intercepted)" {
    check_collective broadcast_perf
}

@test "NCCL single-GPU reduce (native and intercepted)" {
    check_collective reduce_perf
}

@test "NCCL single-GPU all-to-all (native and intercepted)" {
    check_collective alltoall_perf
}

# bats test_tags=multi
@test "NCCL multi-GPU collectives (native and intercepted)" {
    skip "Multi-GPU NCCL tests are disabled until a multi-GPU CI runner is available"
    for binary in all_reduce_perf all_gather_perf reduce_scatter_perf; do
        check_collective "$binary" 2
    done
}
