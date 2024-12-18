#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile

load helpers/utils
load helpers/daemon

load_lib support
load_lib assert
load_lib file

export CEDANA_PROFILING_ENABLED=true

@test "run process (profiling)" {
    jid=$(unix_nano)

    run cedana -P "$PORT" run process echo hello --jid "$jid"

    assert_success
    assert_output --partial "total"
}

@test "run process (profiling output off)" {
    jid=$(unix_nano)

    run cedana -P "$PORT" run process echo hello --jid "$jid" --profiling=false

    assert_success
    refute_output --partial "total"
}

# @test "restore process (profiling)" {
# }
