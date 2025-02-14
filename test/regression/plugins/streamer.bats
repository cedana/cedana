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
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"

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

@test "stream dump process (4 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 4 --compression none
    assert_success

    assert_exists "/tmp/$name"
    assert_exists "/tmp/$name/img-0"
    assert_exists "/tmp/$name/img-1"
    assert_exists "/tmp/$name/img-2"
    assert_exists "/tmp/$name/img-3"

    run kill $pid
}

@test "stream dump process (8 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 8 --compression none
    assert_success

    assert_exists "/tmp/$name"
    assert_exists "/tmp/$name/img-0"
    assert_exists "/tmp/$name/img-1"
    assert_exists "/tmp/$name/img-2"
    assert_exists "/tmp/$name/img-3"
    assert_exists "/tmp/$name/img-4"
    assert_exists "/tmp/$name/img-5"
    assert_exists "/tmp/$name/img-6"
    assert_exists "/tmp/$name/img-7"

    run kill $pid
}

@test "stream dump process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 2 --compression tar
    assert_success

    # tar does no compression, but since the option is valid for non-stream dump,
    # it just creates uncompressed files

    assert_exists "/tmp/$name"
    assert_exists "/tmp/$name/img-0"
    assert_exists "/tmp/$name/img-1"

    run kill $pid
}

@test "stream dump process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 2 --compression gzip
    assert_success

    assert_exists "/tmp/$name"
    assert_exists "/tmp/$name/img-0.gz"
    assert_exists "/tmp/$name/img-1.gz"

    run kill $pid
}

@test "stream dump process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 2 --compression lz4
    assert_success

    assert_exists "/tmp/$name"
    assert_exists "/tmp/$name/img-0.lz4"
    assert_exists "/tmp/$name/img-1.lz4"

    run kill $pid
}

@test "stream dump process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 2 --compression zlib
    assert_success

    assert_exists "/tmp/$name"
    assert_exists "/tmp/$name/img-0.zlib"
    assert_exists "/tmp/$name/img-1.zlib"

    run kill $pid
}

@test "stream dump process (invalid compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 2 --compression jibberish
    assert_failure

    assert_not_exists "/tmp/$name"

    run kill $pid
}

###############
### Restore ###
###############

@test "stream restore process" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!

    run cedana dump process $pid --stream 1 --compression none
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"

    run cedana restore process --path "$dump_file" --stream 1
    assert_success

    run kill $pid
}

@test "stream restore process (custom name)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 1 --compression none
    assert_success

    dump_file="/tmp/$name"
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"

    run cedana restore process --path "$dump_file" --stream 1
    assert_success

    run kill $pid
}

@test "stream restore process (0 parallelism = no streaming)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 0 --compression none
    assert_success

    dump_file="/tmp/$name"
    assert_exists "$dump_file"
    assert_not_exists "$dump_file/img-0"

    run cedana restore process --path "$dump_file" --stream 0
    assert_success

    run kill $pid
}

@test "stream restore process (4 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 4 --compression none
    assert_success

    dump_file="/tmp/$name"
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"
    assert_exists "$dump_file/img-1"
    assert_exists "$dump_file/img-2"
    assert_exists "$dump_file/img-3"

    run cedana restore process --path "$dump_file" --stream 4
    assert_success

    run kill $pid
}

@test "stream restore process (8 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 8 --compression none
    assert_success

    dump_file="/tmp/$name"
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"
    assert_exists "$dump_file/img-1"
    assert_exists "$dump_file/img-2"
    assert_exists "$dump_file/img-3"
    assert_exists "$dump_file/img-4"
    assert_exists "$dump_file/img-5"
    assert_exists "$dump_file/img-6"
    assert_exists "$dump_file/img-7"

    run cedana restore process --path "$dump_file" --stream 8
    assert_success

    run kill $pid
}

@test "stream restore process (mismatched parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 2
    assert_success

    dump_file="/tmp/$name"
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"
    assert_exists "$dump_file/img-1"

    run cedana restore process --path "$dump_file" --stream 3
    assert_failure

    run kill $pid
}

@test "stream restore process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 2 --compression tar
    assert_success

    dump_file="/tmp/$name"
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"
    assert_exists "$dump_file/img-1"

    run cedana restore process --path "$dump_file" --stream 2
    assert_success

    run kill $pid
}

@test "stream restore process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 2 --compression gzip
    assert_success

    dump_file="/tmp/$name"
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0.gz"
    assert_exists "$dump_file/img-1.gz"

    run cedana restore process --path "$dump_file" --stream 2
    assert_success

    run kill $pid
}

@test "stream restore process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 2 --compression lz4
    assert_success

    dump_file="/tmp/$name"
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0.lz4"
    assert_exists "$dump_file/img-1.lz4"

    run cedana restore process --path "$dump_file" --stream 2
    assert_success

    run kill $pid
}

@test "stream restore process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 2 --compression zlib
    assert_success

    dump_file="/tmp/$name"
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0.zlib"
    assert_exists "$dump_file/img-1.zlib"

    run cedana restore process --path "$dump_file" --stream 2
    assert_success

    run kill $pid
}
