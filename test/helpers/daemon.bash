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
        follow_daemon_log "$CEDANA_ADDRESS" 0 # from now on, for this test
    fi
}

teardown_daemon() {
    if ! env_exists "PERSIST_DAEMON"; then
        stop_daemon_at "$SOCK"
    fi
    unfollow_daemon_log
}

##################
# DAEMON HELPERS #
##################

start_daemon_at() {
    local sock=$1
    id=$(basename "$sock")
    debug_log "Starting daemon at socket $sock with config $CEDANA_CONFIG_DIR/config.json"
    # Only to its log file: the daemon (and anything it spawns) must not hold the test's or the
    # runner's output open, or the parallel runner fails the file once they outlive it.
    # Tests see the log through follow_daemon_log.
    cedana daemon start --init-config >"$(daemon_log_file "$sock")" 2>&1 </dev/null 3>&- &
    echo $! >"$(daemon_pid_file "$sock")"
    wait_for_start "$sock"
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

wait_for_start() {
    local sock=$1
    local i=0
    while [ ! -S "$sock" ]; do
        sleep 1
        i=$((i + 1))
        if [ $i -gt $WAIT_TIMEOUT ]; then
            error_log "Daemon failed to start after $WAIT_TIMEOUT seconds"
            return 1
        fi
    done
}

stop_daemon_at() {
    local sock=$1
    if [ ! -e "$sock" ] || [ ! -S "$sock" ]; then
        debug_log "Socket $sock does not exist, skipping stop"
        return 0
    fi
    debug_log "Stopping daemon at socket $sock"
    kill_at_sock "$sock" TERM
    wait_for_stop "$sock"
}

# Waits for the daemon process to exit, not just its socket, which goes away first
wait_for_stop() {
    local sock=$1
    local pid
    pid=$(cat "$(daemon_pid_file "$sock")" 2>/dev/null)
    local i=0
    while [ -S "$sock" ] || { [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; }; do
        sleep 0.2
        i=$((i + 1))
        if [ $i -gt $((WAIT_TIMEOUT * 5)) ]; then
            error_log "Daemon failed to stop after $WAIT_TIMEOUT seconds"
            return 1
        fi
    done
}

daemon_log_file() {
    local sock=$1
    id=$(basename "$sock")
    echo /tmp/cedana-daemon-"$id".log
}

daemon_pid_file() {
    local sock=$1
    id=$(basename "$sock")
    echo /tmp/cedana-daemon-"$id".pid
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
