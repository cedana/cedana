#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=remote,storage:cedana,streamer

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
@test "remote stream dump process (4 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 4 --compression none

    run kill $pid
}

# bats test_tags=dump
@test "remote stream dump process (8 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 8 --compression none

    run kill $pid
}

# bats test_tags=dump
@test "remote stream dump process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 2 --compression tar

    run kill $pid
}

# bats test_tags=dump
@test "remote stream dump process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 2 --compression gzip

    run kill $pid
}

# bats test_tags=dump
@test "remote stream dump process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 2 --compression lz4

    run kill $pid
}

# bats test_tags=dump
@test "remote stream dump process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 2 --compression zlib

    run kill $pid
}

# bats test_tags=dump
@test "remote stream dump process (no compression, leave running)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)
    name2=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 2 --compression none --leave-running

    pid_exists $pid

    sleep 1

    cedana dump process $pid --name "$name2" --dir cedana://ci --streams 2 --compression none

    run kill $pid
}

# bats test_tags=dump
@test "remote stream dump process (gzip compression, leave running)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)
    name2=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 2 --compression gzip --leave-running

    pid_exists $pid

    sleep 1

    cedana dump process $pid --name "$name2" --dir cedana://ci --streams 2 --compression gzip

    run kill $pid
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "remote stream restore process (4 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 4 --compression none

    dump_file="cedana://ci/$name"

    cedana restore process --path "$dump_file"

    run kill $pid
}

# bats test_tags=restore
@test "remote stream restore process (8 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 8 --compression none

    dump_file="cedana://ci/$name"

    cedana restore process --path "$dump_file"

    run kill $pid
}

# bats test_tags=restore
@test "remote stream restore process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 2 --compression tar

    dump_file="cedana://ci/$name"

    cedana restore process --path "$dump_file"

    run kill $pid
}

# bats test_tags=restore
@test "remote stream restore process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 2 --compression gzip

    dump_file="cedana://ci/$name"

    cedana restore process --path "$dump_file"

    run kill $pid
}

# bats test_tags=restore
@test "remote stream restore process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 2 --compression lz4

    dump_file="cedana://ci/$name"

    cedana restore process --path "$dump_file"

    run kill $pid
}

# bats test_tags=restore
@test "remote stream restore process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir cedana://ci --streams 2 --compression zlib

    dump_file="cedana://ci/$name"

    cedana restore process --path "$dump_file"

    run kill $pid
}
