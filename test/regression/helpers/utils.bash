#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

export WORKLOADS="test/workloads"

export KIBIBYTE=1024
export MEBIBYTE=$(( KIBIBYTE * 1024 ))
export GIBIBYTE=$(( MEBIBYTE * 1024 ))

load_lib() {
    load /usr/lib/bats/bats-"$1"/load
}

unix_nano() {
    date +%s%N
}

random_free_port() {
    while true; do
        PORT=$(( ( RANDOM % 64511 ) + 1024 ));
        if ! ss -lntu | grep -q ":$PORT"; then
            echo $PORT; break;
        fi;
    done
}

random_sock() {
    echo "/tmp/$(unix_nano)"
}

pid_from_port() {
    local port=$1
    lsof -t -i:"$port"
}

kill_at_port() {
    local port=$1
    local signal=${2:-9}
    pid=$(pid_from_port "$port")
    kill -"$signal" "$pid"
}

pid_from_sock() {
    local sock=$1
    fuser "$sock" | awk '{print $2}'
}

kill_at_sock() {
    local sock=$1
    local signal=${2:-9}
    fuser "$sock" -k -"$signal"
}

env_exists() {
    local var=$1
    [ -n "${!var}" ]
}

cmd_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Execute a function only once
# Usage: do_once <function_name>
# If the function is currently running, other calls to do_once will wait for it to finish
# If the function is already finished, it will not be executed again
do_once() {
    local func="$1"
    local lock="/tmp/$1.lock"

    if ! mkdir "$lock" 2>/dev/null; then
        while [ ! -d "$lock.done" ]; do
            sleep 0.2
        done
        return
    fi

    "$func"
    mkdir "$lock.done"
}

pid_exists() {
    local pid=$1
    if [ -z "$pid" ]; then
        return 1
    fi
    kill -0 "$pid" 2>/dev/null
}

wait_for_pid() {
    local pid=$1
    local timeout=${2:-10}
    local interval=${3:-0.1}
    local elapsed=0

    while ! kill -0 "$pid" 2>/dev/null; do
        if (( $(echo "$elapsed >= $timeout" | bc -l) )); then
            return 1
        fi
        sleep "$interval"
        elapsed=$(echo "$elapsed + $interval" | bc)
    done

    return 0
}
