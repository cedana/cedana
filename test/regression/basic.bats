#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=base,basic

load helpers/utils
load helpers/daemon

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

@test "cedana --version" {
    expected_version=$(git describe --tags --always)

    run cedana --version
    assert_success
    assert_output --partial "$expected_version"
}

@test "cedana ps" {
    jid=$(unix_nano)
    run cedana exec echo hello --jid "$jid"
    assert_success

    run cedana ps
    assert_success
    assert_output --partial "$jid"
}

@test "Health check" {
    run cedana daemon check
    assert_success
}

@test "Health check (full)" {
    run cedana daemon check --full
    assert_success
}
