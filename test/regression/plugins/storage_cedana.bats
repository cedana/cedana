#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=remote,storage:cedana

load ../../helpers/utils
load ../../helpers/daemon

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

    cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"

    sleep 1

    cedana dump job "$jid" --dir cedana://ci

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "remote dump process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression tar --dir cedana://ci

    run kill $pid
}

# bats test_tags=dump
@test "remote dump process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression gzip --dir cedana://ci

    run kill $pid
}

# bats test_tags=dump
@test "remote dump process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression lz4 --dir cedana://ci

    run kill $pid
}

# bats test_tags=dump
@test "remote dump process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression zlib --dir cedana://ci

    run kill $pid
}

# bats test_tags=dump
@test "remote dump process (no compression, leave running)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)
    name2=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --compression none --leave-running

    pid_exists $pid

    sleep 1

    cedana dump process $pid --name "$name2" --dir cedana://ci --compression none

    run kill $pid
}

# bats test_tags=dump
@test "remote dump process (gzip compression, leave running)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)
    name2=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --compression gzip --leave-running

    pid_exists $pid

    sleep 1

    cedana dump process $pid --name "$name2" --dir cedana://ci --compression gzip

    run kill $pid
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "remote restore process (new job)" {
    jid=$(unix_nano)

    cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"

    sleep 1

    cedana dump job "$jid" --dir cedana://ci

    cedana restore job "$jid"

    run cedana job kill "$jid"
}

# bats test_tags=restore
@test "remote restore process (new job, without daemon)" {
    jid=$(unix_nano)
    code=42

    cedana run process "$WORKLOADS/date-loop.sh" 7 $code --jid "$jid"

    sleep 1

    cedana dump job "$jid" --dir cedana://ci --name "$jid"

    run cedana restore process --path "cedana://ci/$jid.tar" --no-server
    assert_equal $status $code
}

# bats test_tags=restore
@test "remote restore process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression tar --dir cedana://ci

    cedana restore process --path "cedana://ci/$name.tar"

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

    sleep 1

    cedana dump process $pid --name "$name" --compression gzip --dir cedana://ci

    cedana restore process --path "cedana://ci/$name.tar.gz"

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

    sleep 1

    cedana dump process $pid --name "$name" --compression lz4 --dir cedana://ci

    cedana restore process --path "cedana://ci/$name.tar.lz4"

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

    sleep 1

    cedana dump process $pid --name "$name" --compression zlib --dir cedana://ci

    cedana restore process --path "cedana://ci/$name.tar.zlib"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}
