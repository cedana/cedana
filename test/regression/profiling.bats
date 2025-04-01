#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile

load helpers/utils
load helpers/daemon

load_lib support
load_lib assert
load_lib file

export CEDANA_PROFILING_ENABLED=true

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

@test "run process (profiling)" {
    jid=$(unix_nano)

    run cedana run process echo hello --jid "$jid"

    assert_success
    assert_output --partial "total"
}

@test "run process (profiling output off)" {
    jid=$(unix_nano)

    run cedana run process echo hello --jid "$jid" --profiling=false

    assert_success
    refute_output --partial "total"
}

# @test "restore process (profiling)" {
# }
