#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile

load helpers/utils
load helpers/daemon

load_lib support
load_lib assert
load_lib file

@test "manage process" {
    jid=$(unix_nano)

    "$WORKLOADS"/date-loop.sh &
    pid=$!

    run cedana -P "$PORT" manage process $pid --jid "$jid"

    assert_success

    run cedana -P "$PORT" ps

    assert_success
    assert_output --partial "$jid"

    kill $pid
}

@test "manage non-existent process" {
    jid=$(unix_nano)

    run cedana -P "$PORT" manage process 999999 --jid "$jid"

    assert_failure

    run cedana -P "$PORT" ps

    assert_success
    refute_output --partial "$jid"
}
