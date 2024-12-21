#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

export PATH="./:$PATH" # ensure binaries are available
export CEDANA_LOG_LEVEL=trace
export CEDANA_PLUGINS_LOCAL_SEARCH_PATH=$PWD
export CEDANA_PROFILING_ENABLED=false

WAIT_TIMEOUT=100

##################
# BATS LIFECYCLE #
##################

# Below setup ensures that a new daemon is started for each 'test'
# using a unique port. This is done to avoid tests interfering
# when running in parallel.
#
# If `PERSIST_DAEMON` is set, the daemon is started once for an
# entire 'file' and the port is exported to the environment. This
# is useful to test scenarios where the daemon is started once
# and multiple tests are run against it.
#
# Everytime a daemon is started, it has a unique database file
# and log file associated with it.

setup_file() {
    if env_exists "PERSIST_DAEMON"; then
        PORT=$(random_free_port)
        start_daemon_at "$PORT"
        export PORT
    fi
}
teardown_file() {
    if env_exists "PERSIST_DAEMON"; then
        stop_daemon_at "$PORT"
    fi
}
setup() {
    if ! env_exists "PERSIST_DAEMON"; then
        PORT=$(random_free_port)
        start_daemon_at "$PORT"
        export PORT
    else
        log_file=$(daemon_log_file "$PORT")
        tail -f "$log_file" &
        export TAIL_PID=$!
    fi
}
teardown() {
    if ! env_exists "PERSIST_DAEMON"; then
        stop_daemon_at "$PORT"
    else
        kill "$TAIL_PID"
    fi
}

##################
# DAEMON HELPERS #
##################

start_daemon_at() {
    local port=$1
    cedana daemon start -P "$port" --db /tmp/cedana-"$port".db | tee "$(daemon_log_file "$port")" &
    wait_for_start "$port"
}

wait_for_start() {
    local port=$1
    local i=0
    while ! cedana --port "$port" ps &> /dev/null; do
        sleep 0.1
        i=$((i + 1))
        if [ $i -gt $WAIT_TIMEOUT ]; then
            echo "Daemon failed to start"
            exit 1
        fi
    done
}

stop_daemon_at() {
    local port=$1
    kill_at_port "$port" TERM
    wait_for_stop "$port"
}

wait_for_stop() {
    local port=$1
    local i=0
    while cedana --port "$port" ps &> /dev/null; do
        sleep 0.1
        i=$((i + 1))
        if [ $i -gt $WAIT_TIMEOUT ]; then
            echo "Daemon failed to stop"
            exit 1
        fi
    done
}

daemon_log_file() {
    local port=$1
    echo /var/log/cedana-daemon-"$port".log
}

pid_for_jid() {
    local port=$1
    local jid=$2
    table=$(cedana -P "$port" ps)
    echo "$table" | awk -v job="$jid" '$1 == job {print $3}'
}

