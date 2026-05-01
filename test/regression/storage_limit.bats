#!/usr/bin/env bats

# The tests here test the local storage limit of the daemon.

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=base,storage_limit

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

export CEDANA_STORAGE_LIMIT=1

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

@test "test storage limit" {
    jid=$(unix_nano)

    cedana run process --jid "$jid" -- python3 "$WORKLOADS/allocate.py"
    sleep 2

    run cedana dump job "$jid" --leave-running
    assert_success
    sleep 2

    run cedana dump job "$jid" --leave-running
    assert_success
    sleep 2

    run cedana dump job "$jid" --leave-running
    assert_failure

    # we should be able to delete a checkpoint
    # and then the next dump should succeed
    checkpoint_id=$(cedana checkpoint list "$jid" | awk 'NR==2 {print $1}')

    run cedana checkpoint delete "$checkpoint_id"
    assert_success
    sleep 2

    run cedana dump job "$jid" --leave-running
    assert_success

    run cedana job kill "$jid"
    run cedana job delete "$jid"
}
