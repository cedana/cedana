#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=base,basic

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

@test "cedana ps" {
    jid=$(unix_nano)
    cedana exec echo hello --jid "$jid"

    run cedana ps
    assert_success
    assert_output --partial "$jid"
}

@test "health check" {
    cedana check
}

@test "health check (daemon)" {
    cedana daemon check
}

@test "health check (full)" {
    cedana check --full
}

@test "health check (full, daemon)" {
    cedana daemon check --full
}
