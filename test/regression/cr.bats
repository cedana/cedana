#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile

load helpers/utils
load helpers/daemon

load_lib support
load_lib assert
load_lib file

############
### Dump ###
############

@test "dump process" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!

    run cedana dump process $pid
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run kill $pid
}

@test "dump process (custom name)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --compression none
    assert_success

    assert_exists "/tmp/$name"

    run kill $pid
}

@test "dump process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --compression tar
    assert_success

    assert_exists "/tmp/$name.tar"

    run kill $pid
}

@test "dump process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --compression gzip
    assert_success

    assert_exists "/tmp/$name.tar.gz"

    run kill $pid
}

@test "dump process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --compression lz4
    assert_success

    assert_exists "/tmp/$name.tar.lz4"

    run kill $pid
}

@test "dump process (invalid compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --compression jibberish
    assert_failure

    assert_not_exists "/tmp/$name"
    assert_not_exists "/tmp/$name.tar"
    assert_not_exists "/tmp/$name.tar.gz"
    assert_not_exists "/tmp/$name.tar.lz4"

    run kill $pid
}

@test "dump process (new job)" {
    jid=$(unix_nano)

    run cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"
    assert_success

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"
}

@test "dump process (manage existing job)" {
    jid=$(unix_nano)

    "$WORKLOADS"/date-loop.sh &
    pid=$!

    run cedana manage process $pid --jid "$jid"
    assert_success

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"
}

@test "dump non-existent process" {
    id=$(unix_nano)
    mkdir -p /tmp/dump-"$id"

    run cedana dump process 999999999 --dir /tmp

    assert_failure

    # check that there are no files inside the dump dir
    [[ -z $(ls /tmp/dump-"$id") ]]

    rm -rf /tmp/dump-"$id"
}

@test "dump non-existent job" {
    run cedana dump job 999999999

    assert_failure
}

###############
### Restore ###
###############

@test "restore process" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!

    run cedana dump process $pid
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore process --path "$dump_file"
    assert_success

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

@test "restore process (compression tar)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --compression tar
    assert_success

    assert_exists "/tmp/$name.tar"

    run cedana restore process --path "/tmp/$name.tar"
    assert_success

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

@test "restore process (compression gzip)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --compression gzip
    assert_success

    assert_exists "/tmp/$name.tar.gz"

    run cedana restore process --path "/tmp/$name.tar.gz"
    assert_success

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

@test "restore process (compression lz4)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --compression lz4
    assert_success

    assert_exists "/tmp/$name.tar.lz4"

    run cedana restore process --path "/tmp/$name.tar.lz4"
    assert_success

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

@test "restore process (invalid compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --compression tar
    assert_success

    mv "/tmp/$name.tar" "/tmp/$name.tar.jibberish"

    run cedana restore process --path "/tmp/$name.tar.jibberish"
    assert_failure

    run kill $pid
}

@test "restore process (new job)" {
    jid=$(unix_nano)

    run cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"
    assert_success

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore job "$jid"
    assert_success

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
}

@test "restore process (manage existing job)" {
    jid=$(unix_nano)
    log="/tmp/restore-$jid.log"

    "$WORKLOADS"/date-loop.sh &> "$log" < /dev/null &
    pid=$!

    run cedana manage process $pid --jid "$jid"
    assert_success

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore job "$jid"
    assert_success

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
}

@test "restore non-existent process" {
    run cedana restore process --path /tmp/non-existent

    assert_failure

    run ps --pid 999999999
    assert_failure
}


@test "restore non-existent job" {
    run cedana restore job 999999999

    assert_failure
}