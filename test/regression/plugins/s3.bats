#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

############
### Dump ###
############

@test "stream dump process" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!

    run cedana dump process $pid --stream 1 --compression none
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists_s3 "$dump_file/img-0"

    run kill $pid
}

@test "stream dump process (custom name)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 1 --compression none
    assert_success

    assert_exists "/tmp/$name"
    assert_exists "/tmp/$name/img-0"

    run kill $pid
}

@test "stream dump process (0 parallelism = no streaming)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 0 --compression none
    assert_success

    assert_exists "/tmp/$name"
    assert_not_exists "/tmp/$name/img-0"

    run kill $pid
}
