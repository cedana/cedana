#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# Soak tests: the same checkpoint/restore many times, to catch failures that only happen
# 1 in N times (races). They run nightly (the soak tag), counting every failure, so the
# result is a failure rate rather than the first failure.
#
# bats file_tags=base,soak

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

SOAK_ITERATIONS=${SOAK_ITERATIONS:-50}

setup_file() {
    setup_file_daemon
}

setup() {
    setup_daemon
}

teardown() {
    stop_workload "${WORKLOAD_PID:-}"
    teardown_daemon
}

teardown_file() {
    teardown_file_daemon
}

# One process dumped and restored again and again: each restore continues the count
# bats test_tags=dump,restore
@test "soak: dump and restore a process, $SOAK_ITERATIONS times in a row" {
    local log
    log=$(mktemp /tmp/cedana-soak-XXXXXX.log)
    "$WORKLOADS"/count-loop.sh >"$log" 2>&1 &
    local pid=$!
    WORKLOAD_PID=$pid # for teardown
    wait_for_cmd 10 grep -q '^COUNT' "$log"

    local i failures=0 first_failure=""
    for i in $(seq 1 "$SOAK_ITERATIONS"); do
        local name before after
        name=$(unix_nano)
        before=$(grep -oE '^COUNT [0-9]+' "$log" | tail -n 1 | awk '{print $2}')
        if ! cedana dump process "$pid" --name "$name" --dir /tmp --compression none >/dev/null 2>&1 ||
            ! cedana restore process --path "/tmp/$name" >/dev/null 2>&1; then
            failures=$((failures + 1))
            first_failure=${first_failure:-$i}
            # Nothing to continue from if the process is gone
            kill -0 "$pid" 2>/dev/null || break
            continue
        fi
        rm -rf "/tmp/$name"
        sleep 1.5
        after=$(grep -oE '^COUNT [0-9]+' "$log" | tail -n 1 | awk '{print $2}')
        if [ -z "$after" ] || [ "$after" -le "${before:-0}" ]; then
            failures=$((failures + 1))
            first_failure=${first_failure:-$i}
        fi
    done

    if [ "$failures" -gt 0 ]; then
        error_log "$failures of $SOAK_ITERATIONS dump/restore cycles failed (first: cycle $first_failure)"
        return 1
    fi
}
