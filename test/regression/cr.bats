#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile

load helpers/utils
load helpers/daemon

load_lib support
load_lib assert
load_lib file

@test "dump process" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!

    run cedana -P "$PORT" dump process $pid

    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
}

# @test "dump non-existent process" {
# }

# @test "dump job (process)" {
# }
