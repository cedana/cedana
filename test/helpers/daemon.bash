#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

export CEDANA_PROTOCOL=${CEDANA_PROTOCOL:-unix}
export CEDANA_DB_REMOTE=${CEDANA_DB_REMOTE:-false}
export CEDANA_LOG_LEVEL=${CEDANA_LOG_LEVEL:-debug}
export CEDANA_LOG_LEVEL_NO_SERVER=$CEDANA_LOG_LEVEL
export CEDANA_PROFILING_ENABLED=${CEDANA_PROFILING_ENABLED:-false}
export CEDANA_METRICS_ENABLED=${CEDANA_METRICS_ENABLED:-false}
export CEDANA_CHECKPOINT_DIR=${CEDANA_CHECKPOINT_DIR:-/tmp}
export CEDANA_CHECKPOINT_COMPRESSION=${CEDANA_CHECKPOINT_COMPRESSION:-none}
export CEDANA_CHECKPOINT_STREAMS=${CEDANA_CHECKPOINT_STREAMS:-0}
export CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-alpha}
export CEDANA_GPU_SHM_SIZE="${CEDANA_GPU_SHM_SIZE:-$((1 * GIBIBYTE))}"

WAIT_TIMEOUT=60

##################
# BATS LIFECYCLE #
##################

# Below setup ensures that a new daemon is started for each 'test'
# using a unique unix socket. This is done to avoid tests interfering
# when running in parallel.
#
# If `PERSIST_DAEMON` is set, the daemon is started once for an
# entire 'file' and the socket is exported to the environment. This
# is useful to test scenarios where the daemon is started once
# and multiple tests are run against it.
#
# Everytime a daemon is started, it has a unique database file
# and log file associated with it.

setup_file_daemon() {
    if env_exists "PERSIST_DAEMON"; then
        SOCK=$(random_sock)
        export SOCK
        CEDANA_CONFIG_DIR="/tmp/cedana-$(basename "$SOCK")"
        export CEDANA_CONFIG_DIR
        export CEDANA_GPU_LOG_DIR="$CEDANA_CONFIG_DIR"
        export CEDANA_GPU_SOCK_DIR="$CEDANA_CONFIG_DIR"
        export CEDANA_DB_PATH=/tmp/cedana-"$(basename "$SOCK")".db
        export CEDANA_ADDRESS="$SOCK"
        debug start_daemon_at "$SOCK"
    fi
}

teardown_file_daemon() {
    if env_exists "PERSIST_DAEMON"; then
        stop_daemon_at "$SOCK"
    fi
}

setup_daemon() {
    if ! env_exists "PERSIST_DAEMON"; then
        SOCK=$(random_sock)
        export SOCK
        CEDANA_CONFIG_DIR="/tmp/cedana-$(basename "$SOCK")"
        export CEDANA_CONFIG_DIR
        export CEDANA_GPU_LOG_DIR="$CEDANA_CONFIG_DIR"
        export CEDANA_GPU_SOCK_DIR="$CEDANA_CONFIG_DIR"
        export CEDANA_DB_PATH=/tmp/cedana-"$(basename "$SOCK")".db
        export CEDANA_ADDRESS="$SOCK"
        debug start_daemon_at "$SOCK"
        follow_daemon_log "$SOCK" +1 # all of it, as it's this test's daemon
    else
        check_daemon_alive "$CEDANA_ADDRESS" || return 1 # e.g. crashed in an earlier test
        follow_daemon_log "$CEDANA_ADDRESS" 0 # from now on, for this test
    fi
}

teardown_daemon() {
    if ! env_exists "PERSIST_DAEMON"; then
        stop_daemon_at "$SOCK"
    fi
    unfollow_daemon_log
    if env_exists "PERSIST_DAEMON"; then
        check_daemon_alive "$CEDANA_ADDRESS" # crashed in this test
    fi
}

# Fails with the end of the daemon's log if it's no longer running (e.g. it panicked), as
# otherwise later tests only see "connection refused"
check_daemon_alive() {
    local sock=$1
    if [ -z "$(recorded_pid "$(daemon_pid_file "$sock")")" ]; then
        error_log "Daemon at $sock is no longer running"
        show_daemon_crash "$(daemon_log_file "$sock")"
        return 1
    fi
}

# Shows the crash from a daemon's log: from a Go panic or fatal error to the end (its whole
# stack trace, whose top would otherwise be cut off), else just the end of the log
show_daemon_crash() {
    local log=$1
    local line
    line=$(grep -nE '^(panic: |fatal error: )' "$log" 2>/dev/null | head -1 | cut -d: -f1)
    if [ -n "$line" ]; then
        tail -n +"$line" "$log"
    else
        tail -n 50 "$log"
    fi
}

##################
# DAEMON HELPERS #
##################

start_daemon_at() {
    local sock=$1
    debug_log "Starting daemon at socket $sock with config $CEDANA_CONFIG_DIR/config.json"
    # Only to its log file: the daemon (and anything it spawns) must not hold the test's or the
    # runner's output open, or the parallel runner fails the file once they outlive it.
    # Tests see the log through follow_daemon_log.
    cedana daemon start --init-config >"$(daemon_log_file "$sock")" 2>&1 </dev/null 3>&- &
    record_pid $! "$(daemon_pid_file "$sock")"
    wait_for_start "$sock" $!
}

# Shows the daemon's log in the test's output, from the given line (+1 for all, 0 for new)
follow_daemon_log() {
    local sock=$1
    local from=$2
    tail -n "$from" -F "$(daemon_log_file "$sock")" 2>/dev/null 3>&- &
    TAIL_PID=$!
}

unfollow_daemon_log() {
    if [ -n "$TAIL_PID" ]; then
        kill "$TAIL_PID" 2>/dev/null
        wait "$TAIL_PID" 2>/dev/null || true
        TAIL_PID=
    fi
}

# Polled often, as it's on the path of every test. Fails right away if the daemon exits.
wait_for_start() {
    local sock=$1
    local pid=$2
    local i=0
    while [ ! -S "$sock" ]; do
        if [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null; then
            error_log "Daemon exited before starting, see $(daemon_log_file "$sock")"
            return 1
        fi
        sleep 0.1
        i=$((i + 1))
        if [ $i -gt $((WAIT_TIMEOUT * 10)) ]; then
            error_log "Daemon failed to start after $WAIT_TIMEOUT seconds"
            return 1
        fi
    done
}

stop_daemon_at() {
    local sock=$1
    if [ ! -S "$sock" ] && [ -z "$(recorded_pid "$(daemon_pid_file "$sock")")" ]; then
        debug_log "Daemon at $sock is not running, skipping stop"
        return 0
    fi
    debug_log "Stopping daemon at socket $sock"
    terminate_at_sock "$sock"
    wait_for_stop "$sock"
}

# Sends TERM to the daemon at sock, by its recorded PID, as finding who holds the socket
# (fuser) scans every process
terminate_at_sock() {
    local sock=$1
    local pid
    pid=$(recorded_pid "$(daemon_pid_file "$sock")")
    if [ -z "$pid" ] || ! kill -TERM "$pid" 2>/dev/null; then
        kill_at_sock "$sock" TERM
    fi
}

# Waits for the daemon process to exit, not just its socket, which goes away first. A daemon
# that crashed leaves its socket behind, so with its PID recorded, only the process counts.
wait_for_stop() {
    local sock=$1
    local pid_file
    pid_file=$(daemon_pid_file "$sock")
    local i=0
    while if [ -s "$pid_file" ]; then [ -n "$(recorded_pid "$pid_file")" ]; else [ -S "$sock" ]; fi; do
        sleep 0.1
        i=$((i + 1))
        if [ $i -gt $((WAIT_TIMEOUT * 10)) ]; then
            error_log "Daemon failed to stop after $WAIT_TIMEOUT seconds"
            return 1
        fi
    done
    rm -f "$sock" # left behind if it crashed
}

daemon_log_file() {
    echo /tmp/cedana-daemon-"$(basename "$1")".log
}

daemon_pid_file() {
    echo /tmp/cedana-daemon-"$(basename "$1")".pid
}

# A process's start time (in clock ticks since boot), which tells it apart from a later
# process reusing its PID
pid_start_time() {
    sed 's/.*) //' "/proc/$1/stat" 2>/dev/null | cut -d' ' -f20
}

record_pid() {
    echo "$1 $(pid_start_time "$1")" >"$2"
}

# The PID recorded in file, if that process is still running (and not another one that
# reused its PID)
recorded_pid() {
    local pid start
    { read -r pid start <"$1"; } 2>/dev/null || return 0
    if [ -n "$pid" ] && [ -n "$start" ] && [ "$(pid_start_time "$pid")" = "$start" ]; then
        echo "$pid"
    fi
}

pid_for_jid() {
    local jid=$1
    table=$(cedana ps)
    echo "$table" | awk -v job="$jid" '$1 == job {print $3}'
}

logfile_for_jid() {
    local jid=$1
    table=$(cedana ps)
    echo "$table" | awk -v job="$jid" '$1 == job {print $NF}'
}

watch_logs() {
    local jid=$1
    log_file=$(logfile_for_jid "$jid")
    pid=$(pid_for_jid "$jid")

    debug tail -f --silent --pid="$pid" "$log_file" &
}
