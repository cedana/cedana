#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=base,fds

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

FD_ZOO=/cedana-samples/cpu_smr/fd_zoo.py

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

# A process tree holding the fd kinds servers use (epoll, eventfd, timerfd, signalfd,
# inotify, pipes, FIFOs, socketpairs, a pty, UDP, unix and TCP connections), each with state
# in flight: all of it must come back after a restore (see fd_zoo.py)
# bats test_tags=dump,restore
@test "restore a process tree holding many kinds of fds, with state in flight" {
    if [ ! -x "$FD_ZOO" ]; then
        skip "fd_zoo.py is not in this image (needs a newer cedana-samples)"
    fi

    local log gate dir
    log=$(mktemp /tmp/cedana-fds-XXXXXX.log)
    gate="$log.gate"
    dir=$(mktemp -d /tmp/cedana-fds-XXXXXX)
    "$FD_ZOO" --log "$log" --dir "$dir" --gate "$gate" &
    local pid=$!
    WORKLOAD_PID=$pid # for teardown
    wait_for_cmd 30 grep -q '^OK' "$log"

    local name lines
    name=$(unix_nano)
    cedana dump process "$pid" --name "$name" --dir /tmp --compression none --tcp-established
    lines=$(wc -l <"$log")
    cedana restore process --path "/tmp/$name" --tcp-established
    rm -rf "/tmp/${name:?}"
    wait_for_cmd 30 "tail -n +$((lines + 1)) $log | grep -q '^OK'" || {
        error_log "no progress after the restore"
        tail -n 10 "$log"
        return 1
    }

    touch "$gate"
    wait_for_cmd 60 grep -qE '^(PASS|MISMATCH)' "$log"
    rm -f "$gate"
    run grep MISMATCH "$log"
    assert_failure
    run grep -q '^PASS' "$log"
    assert_success
}
