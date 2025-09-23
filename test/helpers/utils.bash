#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

export WORKLOADS="test/workloads"

export KIBIBYTE=1024
export MEBIBYTE=$(( KIBIBYTE * 1024 ))
export GIBIBYTE=$(( MEBIBYTE * 1024 ))

export RED='\033[0;31m'
export NC='\033[0m' # No Color

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

path_exists() {
    local path=$1
    [ -e "$path" ]
}

env_exists() {
    local var=$1
    [ -n "${!var}" ]
}

check_env() {
    local var=$1
    if ! env_exists "$var"; then
        error_log "Environment variable '$var' is not set."
        exit 1
    fi
}

cmd_exists() {
    command -v "$1" >/dev/null 2>&1
}

check_cmd() {
    local cmd=$1
    if ! cmd_exists "$cmd"; then
        error_log "Command '$cmd' is not available."
        exit 1
    fi
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
    local timeout=${2:-60}
    local interval=1
    local elapsed=0

    while ! kill -0 "$pid" 2>/dev/null; do
        if (( elapsed >= timeout )); then
            return 1
        fi
        sleep "$interval"
        ((elapsed += interval))
    done

    return 0
}

# Wait for a cmd to start returning zero exit code, then return the output.
wait_for_cmd() {
    local timeout=${1:-60}
    local interval=1
    shift 1
    local elapsed=0
    debug_log "Waiting for '$*' (timeout: $timeout seconds)"
    # Remove quotes from single argument case
    if [ "$#" -eq 1 ]; then
        local cmd=$1
        while ! eval "$cmd" 2> /dev/null; do
            if (( elapsed >= timeout )); then
                error_log "Timed out waiting for '$cmd' to succeed after $timeout seconds"
                return 1
            fi
            sleep "$interval"
            ((elapsed += interval))
        done
    else
        while ! "$@" 2> /dev/null; do
            if (( elapsed >= timeout )); then
                error_log "Timed out waiting for '$*' to succeed after $timeout seconds"
                return 1
            fi
            sleep "$interval"
            ((elapsed += interval))
        done
    fi
    debug_log "'$*' succeeded after $elapsed seconds"

    return 0
}

# Same as wait_for_cmd, but waits for the command to fail instead of succeed.
wait_for_cmd_fail() {
    local timeout=${1:-60}
    local interval=1
    shift 1
    local elapsed=0
    debug_log "Waiting for '$*' to fail (timeout: $timeout seconds)"
    # Remove quotes from single argument case
    if [ "$#" -eq 1 ]; then
        local cmd=$1
        while eval "$cmd" 2> /dev/null; do
            if (( elapsed >= timeout )); then
                error_log "Timed out waiting for '$cmd' to fail after $timeout seconds"
                return 1
            fi
            sleep "$interval"
            ((elapsed += interval))
        done
    else
        while "$@" 2> /dev/null; do
            if (( elapsed >= timeout )); then
                error_log "Timed out waiting for '$*' to fail after $timeout seconds"
                return 1
            fi
            sleep "$interval"
            ((elapsed += interval))
        done
    fi
    debug_log "'$*' failed (as expected) after $elapsed seconds"

    return 0
}


debug_log() {
    local message="$1"
    if [ "$DEBUG" == "1" ]; then
        echo "[DEBUG] $message" >&3
    fi
}

error_log() {
    local message="$1"
    if [ "$DEBUG" == "1" ]; then
        echo -e "${RED}[ERROR] $message${NC}" >&3
    else
        echo -e "${RED}[ERROR] $message${NC}" >&2
    fi
}

debug() {
    if [ "$DEBUG" == "1" ]; then
        if [ "$#" -eq 1 ]; then
            eval "$1" >&3 2>&1
        else
            "$@" >&3 2>&1
        fi
    else
        if [ "$#" -eq 1 ]; then
            eval "$1" >&2
        else
            "$@" >&2
        fi
    fi
}

error() {
    if [ "$DEBUG" == "1" ]; then
        if [ "$#" -eq 1 ]; then
            eval "$1" >&3 2>&1
        else
            "$@" >&3 2>&1
        fi
    else
        if [ "$#" -eq 1 ]; then
            eval "$1" >&2
        else
            "$@" >&2
        fi
    fi
}
