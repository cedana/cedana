#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

export PATH="./:$PATH"
export CEDANA_LOG_LEVEL=trace
export CEDANA_PLUGINS_LOCAL_SEARCH_PATH=$PWD

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
    cedana daemon start -P "$port" --local-db /tmp/cedana-"$port".db | tee "$(daemon_log_file "$port")" &
    wait_for_start "$port"
}

wait_for_start() {
    local port=$1
    CEDANA_CLI_WAIT_FOR_READY=true cedana -P "$port" ps
}

stop_daemon_at() {
    local port=$1
    kill_at_port "$port" TERM
    wait_for_stop "$port"
}

wait_for_stop() {
    local port=$1
    while cedana --port "$port" ps &> /dev/null; do
        sleep 0.1
    done
}

daemon_log_file() {
    local port=$1
    echo /var/log/cedana-daemon-"$port".log
}
