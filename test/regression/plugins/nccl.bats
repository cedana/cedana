#!/usr/bin/env bats

# Exclude nccl from the general gpu suite; its dedicated step runs serially.
# bats file_tags=gpu,nccl

load ../../helpers/utils
load ../../helpers/daemon

load_lib support
load_lib assert

export CEDANA_CHECKPOINT_COMPRESSION=gzip

setup_file() {
    setup_file_daemon
}

setup() {
    setup_daemon
    jid="nccl-$(unix_nano)"
}

teardown() {
    timeout --kill-after=5s 15s cedana job kill "$jid" >/dev/null 2>&1 || true
    rm -rf -- "$BATS_TEST_TMPDIR/checkpoint"
    if env_exists PERSIST_DAEMON; then
        # Stop this test's log tail immediately; the shared daemon stays alive
        # until teardown_file, which can be more than WAIT_TIMEOUT away.
        kill "$TAIL_PID"
    else
        teardown_daemon
    fi
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

wait_for_collective() {
    local log_file=$1 offset=${2:-0}
    for _ in {1..30}; do
        # These collectives emit 13 columns, with out-of-place/in-place error
        # counts in columns 9 and 13. Ignore headers and unfinished rows.
        if [ -f "$log_file" ] && tail -c "+$((offset + 1))" "$log_file" | awk '
            NF == 13 && $1 == 1048576 && $3 == "float" {
                seen = 1
                if ($9 != "0" || $13 != "0") bad = 1
            }
            END { exit !(seen && !bad) }
        '; then
            return 0
        fi
        sleep 1
    done
    fail "NCCL did not produce fresh zero-error collective results"
}

check_restore() {
    local binary="/opt/nccl-tests/$1" log_file old_log offset cycle
    cedana run process --gpu-enabled --jid "$jid" -- "$binary" \
        -g 1 -b 1M -e 1M -n 100 -c 1 -N 0 -T 30
    log_file=$(logfile_for_jid "$jid")
    wait_for_collective "$log_file"

    for cycle in 1 2; do
        mkdir -p "$BATS_TEST_TMPDIR/checkpoint/$cycle"
        run timeout --kill-after=5s 90s cedana dump job "$jid" \
            --dir "$BATS_TEST_TMPDIR/checkpoint/$cycle"
        assert_success
        old_log=$log_file
        offset=$(stat -c %s "$log_file")

        run timeout --kill-after=5s 90s cedana restore job "$jid"
        assert_success
        log_file=$(logfile_for_jid "$jid")
        [ "$log_file" = "$old_log" ] || offset=0
        wait_for_collective "$log_file" "$offset"
    done
}

# bats test_tags=restore,crcr
@test "NCCL single-GPU all-reduce repeated checkpoint/restore" {
    check_restore all_reduce_perf
}

# bats test_tags=restore,crcr
@test "NCCL single-GPU all-gather repeated checkpoint/restore" {
    check_restore all_gather_perf
}

# bats test_tags=restore,crcr
@test "NCCL single-GPU reduce-scatter repeated checkpoint/restore" {
    check_restore reduce_scatter_perf
}

# bats test_tags=multi
@test "NCCL multi-GPU collectives (native and intercepted)" {
    skip "Multi-GPU NCCL tests are disabled until a multi-GPU CI runner is available"
    for binary in all_reduce_perf all_gather_perf reduce_scatter_perf; do
        check_collective "$binary" 2
    done
}
