#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=base,manage

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

setup_file() {
    setup_file_daemon
}

setup() {
    setup_daemon
}

teardown() {
    teardown_daemon
}

teardown_file() {
    teardown_file_daemon
}

@test "manage process" {
    jid=$(unix_nano)

    "$WORKLOADS"/date-loop.sh &
    pid=$!

    cedana manage process $pid --jid "$jid"

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    kill $pid
}

@test "manage process (upcoming)" {
    pid=$(unix_nano)

    run cedana manage process "$pid" --upcoming
    assert_failure # not possible for linux processes
}

@test "manage non-existent process" {
    jid=$(unix_nano)

    run cedana manage process 999999 --jid "$jid"
    assert_failure

    run cedana ps
    assert_success
    refute_output --partial "$jid"
}
