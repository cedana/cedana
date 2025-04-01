#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

export PATH="./:$PATH" # ensure binaries are available
export CEDANA_PROTOCOL="unix"
export CEDANA_LOG_LEVEL=debug
export CEDANA_PROFILING_ENABLED=false
export CEDANA_CHECKPOINT_COMPRESSION=none

WAIT_TIMEOUT=100

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

setup_suite() {
    if env_exists "PERSIST_DAEMON"; then
        SOCK=$(random_sock)
        start_daemon_at "$SOCK"
        export CEDANA_ADDRESS="$SOCK"
    fi
}
teardown_suite() {
    if env_exists "PERSIST_DAEMON"; then
        stop_daemon_at "$SOCK"
    fi
}
setup() {
    if ! env_exists "PERSIST_DAEMON"; then
        SOCK=$(random_sock)
        start_daemon_at "$SOCK"
        export CEDANA_ADDRESS="$SOCK"
    else
        log_file=$(daemon_log_file "$CEDANA_ADDRESS")
        tail -f "$log_file" &
        export TAIL_PID=$!
    fi
}
teardown() {
    if ! env_exists "PERSIST_DAEMON"; then
        stop_daemon_at "$SOCK"
    else
        kill "$TAIL_PID"
    fi
}

##################
# DAEMON HELPERS #
##################

start_daemon_at() {
    local sock=$1
    id=$(basename "$sock")
    cedana daemon start --address "$sock" --db /tmp/cedana-"$id".db | tee "$(daemon_log_file "$sock")" &
    wait_for_start "$sock"
}

wait_for_start() {
    local sock=$1
    local i=0
    while ! cedana --address "$sock" ps &>/dev/null; do
        sleep 0.1
        i=$((i + 1))
        if [ $i -gt $WAIT_TIMEOUT ]; then
            echo "Daemon failed to start"
            exit 1
        fi
    done
}

stop_daemon_at() {
    local sock=$1
    kill_at_sock "$sock" TERM
    wait_for_stop "$sock"
}

wait_for_stop() {
    local sock=$1
    local i=0
    while cedana --address "$sock" ps &>/dev/null; do
        sleep 0.1
        i=$((i + 1))
        if [ $i -gt $WAIT_TIMEOUT ]; then
            echo "Daemon failed to stop"
            exit 1
        fi
    done
}

daemon_log_file() {
    local sock=$1
    id=$(basename "$sock")
    echo /var/log/cedana-daemon-"$id".log
}

pid_for_jid() {
    local jid=$1
    table=$(cedana ps)
    echo "$table" | awk -v job="$jid" '$1 == job {print $3}'
}

check_shm_size() {
    local jid=$1
    local expected_size=$2
    local shm_file="/dev/shm/cedana-gpu.$jid"

    if [[ ! -f "$shm_file" ]]; then
        echo "Error: $shm_file not found."
        return 1
    fi

    local actual_size
    actual_size=$(stat --format="%s" "$shm_file")

    if [[ "$actual_size" -ne "$expected_size" ]]; then
        echo "Size mismatch: expected $expected_size, but got $actual_size"
        return 1
    fi

    echo "Size check passed for $shm_file"
    return 0
}
