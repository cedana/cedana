#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=base,profiling

load ../helpers/utils
load ../helpers/daemon

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

# bats test_tags=serverless
@test "run process (profiling, without daemon)" {
    jid=$(unix_nano)

    run cedana run process echo hello --jid "$jid" --no-server
    assert_success
    assert_output --partial "total"
}

# bats test_tags=serverless
@test "run process (profiling output off, without daemon)" {
    jid=$(unix_nano)

    run cedana run process echo hello --jid "$jid" --profiling=false --no-server
    assert_success
    refute_output --partial "total"
}

# bats test_tags=dump
@test "dump process (profiling)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!

    run cedana dump process $pid
    assert_success
    assert_output --partial "total"

    run kill $pid
}

# bats test_tags=dump
@test "dump process (profiling output off)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!

    run cedana dump process $pid --profiling=false
    assert_success
    refute_output --partial "total"

    run kill $pid
}

# bats test_tags=restore
@test "restore process (profiling)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!

    run cedana dump process $pid
    assert_success
    assert_output --partial "total"

    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')

    run cedana restore process --path "$dump_file"
    assert_success
    assert_output --partial "total"

    run kill $pid
}

# bats test_tags=restore
@test "restore process (profiling output off)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!

    run cedana dump process $pid --profiling=false
    assert_success
    refute_output --partial "total"

    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')

    run cedana restore process --path "$dump_file" --profiling=false
    assert_success
    refute_output --partial "total"

    run kill $pid
}

# bats test_tags=restore,serverless
@test "restore process (profiling, without daemon)" {
    "$WORKLOADS"/date-loop.sh 3 &
    pid=$!

    run cedana dump process $pid
    assert_success
    assert_output --partial "total"

    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')

    run cedana restore process --path "$dump_file" --no-server
    assert_success
    assert_output --partial "total"

    run kill $pid
}

# bats test_tags=restore,serverless
@test "restore process (profiling output off, without daemon)" {
    "$WORKLOADS"/date-loop.sh 3 &
    pid=$!

    run cedana dump process $pid --profiling=false
    assert_success
    refute_output --partial "total"

    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')

    run cedana restore process --path "$dump_file" --profiling=false --no-server
    assert_success
    refute_output --partial "total"

    run kill $pid
}
