#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=base,ipc

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

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

# Processes passing messages through shared memory and a queue (pipe, POSIX semaphores), as
# an inference server's do: messages in flight at the checkpoint must all arrive after the
# restore, in order, with their payloads (customer issue P-49)
# bats test_tags=dump,restore
@test "restore processes with messages in flight through shared memory" {
    local log
    log=$(mktemp /tmp/cedana-ipc-XXXXXX.log)
    "$WORKLOADS"/shm-messages.py "$log" &
    local pid=$!
    WORKLOAD_PID=$pid # for teardown
    wait_for_cmd 30 grep -q '^SEQ' "$log"

    local cycle
    for cycle in 1 2; do
        local name lines
        name=$(unix_nano)
        cedana dump process "$pid" --name "$name" --dir /tmp --compression none
        lines=$(wc -l <"$log")
        cedana restore process --path "/tmp/$name"

        # The consumer keeps receiving, with nothing lost, reordered or corrupted
        wait_for_cmd 30 "tail -n +$((lines + 1)) $log | grep -q '^SEQ'" || {
            error_log "cycle $cycle: no messages received after the restore"
            tail -n 5 "$log"
            return 1
        }
        run grep MISMATCH "$log"
        assert_failure
    done

    pkill -TERM -P "$pid" || true
    kill -TERM "$pid" 2>/dev/null || true
}
