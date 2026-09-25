#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

csx_daemon_log_file() {
    local sock=$1
    id=$(basename "$sock")
    echo /tmp/csx-daemon-"$id".log
}

wait_for_csx_start() {
    local sock=$1
    local i=0
    while [ ! -S "$sock" ]; do
        sleep 1
        i=$((i + 1))
        if [ $i -gt $WAIT_TIMEOUT ]; then
            error_log "CSX Daemon failed to start after $WAIT_TIMEOUT seconds"
            exit 1
        fi
    done
}

start_csx_daemon_at() {
    local sock=$1
    check_cmd csx
    debug_log "Starting csx daemon at socket $sock"
    csx | tee "$(csx_daemon_log_file "$sock")" &
    wait_for_csx_start "$sock"
}

stop_csx_daemon() {
    local sock=$1
    if [ ! -e "$sock" ] || [ ! -S "$sock" ]; then
        debug_log "Socket $sock does not exist, skipping stop"
        return 0
    fi
    debug_log "Stopping daemon at socket $sock"
    kill_at_sock "$sock" TERM
    wait_for_stop "$sock"
}

# Below setup ensures that a new csx daemon is started for each 'test'
# using a unique unix socket, alongside the cedana daemon.
#
# If `PERSIST_DAEMON` is set, the csx daemon is started once for an
# entire 'file' instead, mirroring the cedana daemon lifecycle.
#
# Everytime a csx daemon is started, it has unique storage paths
# and a log file associated with it.

start_csx_daemon() {
    CSX_SOCK=$(random_sock)
    export CSX_SOCK
    export CEDANA_CSX_SOCK_ADDR="$CSX_SOCK"
    export CEDANA_CSX_NFS_PATH=/tmp/csx-"$(basename "$CSX_SOCK")"
    export CEDANA_CSX_TMP_DIR=/cedana/tmpdir-"$(basename "$CSX_SOCK")"
    export CEDANA_CSX_MEMORY_CACHE_PATH=/cedana/memstore-"$(basename "$CSX_SOCK")"
    export CEDANA_CSX_LOCAL_DISK_CACHE_PATH=/cedana/diskstore-"$(basename "$CSX_SOCK")"
    debug start_csx_daemon_at "$CSX_SOCK"
}

cleanup_csx_daemon() {
    stop_csx_daemon "$CSX_SOCK"
    rm -rf /tmp/csx-"$(basename "$CSX_SOCK")"
    rm -rf /cedana/tmpdir-"$(basename "$CSX_SOCK")"
    rm -rf /cedana/memstore-"$(basename "$CSX_SOCK")"
    rm -rf /cedana/diskstore-"$(basename "$CSX_SOCK")"
    rm -f "$(csx_daemon_log_file "$CSX_SOCK")"
}

setup_file_csx_daemon() {
    if env_exists "PERSIST_DAEMON"; then
        start_csx_daemon
    fi
}

teardown_file_csx_daemon() {
    if env_exists "PERSIST_DAEMON"; then
        cleanup_csx_daemon
    fi
}

setup_csx_daemon() {
    if ! env_exists "PERSIST_DAEMON"; then
        start_csx_daemon
    else
        log_file=$(csx_daemon_log_file "$CSX_SOCK")
        tail -f "$log_file" &
        CSX_TAIL_PID=$!
        export CSX_TAIL_PID
    fi
}

teardown_csx_daemon() {
    if ! env_exists "PERSIST_DAEMON"; then
        cleanup_csx_daemon
    else
        if [ -n "$CSX_TAIL_PID" ]; then
            wait_for_stop "$CSX_SOCK" && kill "$CSX_TAIL_PID" &
        fi
    fi
}
