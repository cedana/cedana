#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

export CEDANA_PROTOCOL=${CEDANA_PROTOCOL:-unix}
export CEDANA_REMOTE=${CEDANA_REMOTE:-false}
export CEDANA_LOG_LEVEL=${CEDANA_LOG_LEVEL:-debug}
export CEDANA_LOG_LEVEL_NO_SERVER=$CEDANA_LOG_LEVEL
export CEDANA_PROFILING_ENABLED=${CEDANA_PROFILING_ENABLED:-false}
export CEDANA_CHECKPOINT_DIR=${CEDANA_CHECKPOINT_DIR:-/tmp}
export CEDANA_CHECKPOINT_COMPRESSION=${CEDANA_CHECKPOINT_COMPRESSION:-none}
export CEDANA_CHECKPOINT_STREAMS=${CEDANA_CHECKPOINT_STREAMS:-0}
export CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-alpha}
export CEDANA_GPU_SHM_SIZE="${CEDANA_GPU_SHM_SIZE:-$((1*GIBIBYTE))}"

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
        CEDANA_CONFIG_DIR="/tmp/cedana-$(basename "$SOCK")"
        export CEDANA_CONFIG_DIR
        export CEDANA_GPU_LOG_DIR="$CEDANA_CONFIG_DIR"
        export CEDANA_GPU_SOCK_DIR="$CEDANA_CONFIG_DIR"
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
        CEDANA_CONFIG_DIR="/tmp/cedana-$(basename "$SOCK")"
        export CEDANA_CONFIG_DIR
        export CEDANA_GPU_LOG_DIR="$CEDANA_CONFIG_DIR"
        export CEDANA_GPU_SOCK_DIR="$CEDANA_CONFIG_DIR"
        export CEDANA_ADDRESS="$SOCK"
        debug start_daemon_at "$SOCK"
    else
        log_file=$(daemon_log_file "$CEDANA_ADDRESS")
        tail -f "$log_file" &
        TAIL_PID=$!
        export TAIL_PID
    fi
}
teardown_daemon() {
    if ! env_exists "PERSIST_DAEMON"; then
        stop_daemon_at "$SOCK"
    else
        if [ -n "$TAIL_PID" ]; then
            kill "$TAIL_PID"
        fi
    fi
}

##################
# DAEMON HELPERS #
##################

start_daemon_at() {
    local sock=$1
    id=$(basename "$sock")
    cedana daemon start --db /tmp/cedana-"$id".db | tee "$(daemon_log_file "$sock")" &
    wait_for_start "$sock"
}

wait_for_start() {
    local sock=$1
    local i=0
    while [ ! -S "$sock" ]; do
        sleep 1
        i=$((i + 1))
        if [ $i -gt $WAIT_TIMEOUT ]; then
            error_log "Daemon failed to start after $WAIT_TIMEOUT seconds"
            exit 1
        fi
    done
}

stop_daemon_at() {
    local sock=$1
    if [ ! -e "$sock" ] || [ ! -S "$sock" ]; then
        debug_log "Socket $sock does not exist, skipping stop"
        return 0
    fi
    kill_at_sock "$sock" TERM
    wait_for_stop "$sock"
}

wait_for_stop() {
    local sock=$1
    local i=0
    while [ -S "$sock" ]; do
        sleep 1
        i=$((i + 1))
        if [ $i -gt $WAIT_TIMEOUT ]; then
            error_log "Daemon failed to stop after $WAIT_TIMEOUT seconds"
            exit 1
        fi
    done
}

daemon_log_file() {
    local sock=$1
    id=$(basename "$sock")
    echo /tmp/cedana-daemon-"$id".log
}

pid_for_jid() {
    local jid=$1
    table=$(cedana ps)
    echo "$table" | awk -v job="$jid" '$1 == job {print $3}'
}
