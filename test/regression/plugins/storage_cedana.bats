#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=remote,storage:cedana

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

############
### Dump ###
############

# bats test_tags=dump
@test "remote dump process (new job)" {
    jid=$(unix_nano)

    run cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"
    assert_success

    run cedana dump job "$jid" --dir cedana://ci
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    run echo "$dump_file"
    assert_output --partial "cedana://ci"

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "remote dump process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --compression tar --dir cedana://ci
    assert_success

    run kill $pid
}

# bats test_tags=dump
@test "remote dump process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --compression gzip --dir cedana://ci
    assert_success

    run kill $pid
}

# bats test_tags=dump
@test "remote dump process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --compression lz4 --dir cedana://ci
    assert_success

    run kill $pid
}

# bats test_tags=dump
@test "remote dump process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --compression zlib --dir cedana://ci
    assert_success

    run kill $pid
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "remote restore process (new job)" {
    jid=$(unix_nano)

    run cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"
    assert_success

    run cedana dump job "$jid" --dir cedana://ci
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    run echo "$dump_file"
    assert_output --partial "cedana://ci"

    run cedana restore job "$jid"
    assert_success

    run cedana job kill "$jid"
}

# bats test_tags=restore
@test "remote restore process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --compression tar --dir cedana://ci
    assert_success

    run cedana restore process --path "cedana://ci/$name.tar"
    assert_success

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "remote restore process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --compression gzip --dir cedana://ci
    assert_success

    run cedana restore process --path "cedana://ci/$name.tar.gz"
    assert_success

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "remote restore process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --compression lz4 --dir cedana://ci
    assert_success

    run cedana restore process --path "cedana://ci/$name.tar.lz4"
    assert_success

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "remote restore process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --compression zlib --dir cedana://ci
    assert_success

    run cedana restore process --path "cedana://ci/$name.tar.zlib"
    assert_success

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}
