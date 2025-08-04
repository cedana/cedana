#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=remote,storage:s3,streamer

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
@test "remote (S3) stream dump process (4 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 4 --compression none
    assert_success

    run kill $pid
}

# bats test_tags=dump
@test "remote (S3) stream dump process (8 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 8 --compression none
    assert_success

    run kill $pid
}

# bats test_tags=dump
@test "remote (S3) stream dump process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 2 --compression tar
    assert_success

    # tar does no compression, but since the option is valid for non-stream dump,
    # it just creates uncompressed files

    run kill $pid
}

# bats test_tags=dump
@test "remote (S3) stream dump process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 2 --compression gzip
    assert_success

    run kill $pid
}

# bats test_tags=dump
@test "remote (S3) stream dump process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 2 --compression lz4
    assert_success

    run kill $pid
}

# bats test_tags=dump
@test "remote (S3) stream dump process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 2 --compression zlib
    assert_success

    run kill $pid
}

# bats test_tags=dump
@test "remote (S3) stream dump process (no compression, leave running)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)
    name2=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 2 --compression none --leave-running
    assert_success

    pid_exists $pid

    sleep 1

    run cedana dump process $pid --name "$name2" --dir s3://checkpoints-ci --streams 2 --compression none
    assert_success

    run kill $pid
}

# bats test_tags=dump
@test "remote (S3) stream dump process (gzip compression, leave running)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)
    name2=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 2 --compression gzip --leave-running
    assert_success

    pid_exists $pid

    sleep 1

    run cedana dump process $pid --name "$name2" --dir s3://checkpoints-ci --streams 2 --compression gzip
    assert_success

    run kill $pid
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "remote (S3) stream restore process (4 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 4 --compression none
    assert_success

    dump_file="s3://checkpoints-ci/$name"

    run cedana restore process --path "$dump_file"
    assert_success

    run kill $pid
}

# bats test_tags=restore
@test "remote (S3) stream restore process (8 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 8 --compression none
    assert_success

    dump_file="s3://checkpoints-ci/$name"

    run cedana restore process --path "$dump_file"
    assert_success

    run kill $pid
}

# bats test_tags=restore
@test "remote (S3) stream restore process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 2 --compression tar
    assert_success

    dump_file="s3://checkpoints-ci/$name"

    run cedana restore process --path "$dump_file"
    assert_success

    run kill $pid
}

# bats test_tags=restore
@test "remote (S3) stream restore process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 2 --compression gzip
    assert_success

    dump_file="s3://checkpoints-ci/$name"

    run cedana restore process --path "$dump_file"
    assert_success

    run kill $pid
}

# bats test_tags=restore
@test "remote (S3) stream restore process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 2 --compression lz4
    assert_success

    dump_file="s3://checkpoints-ci/$name"

    run cedana restore process --path "$dump_file"
    assert_success

    run kill $pid
}

# bats test_tags=restore
@test "remote (S3) stream restore process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --streams 2 --compression zlib
    assert_success

    dump_file="s3://checkpoints-ci/$name"

    run cedana restore process --path "$dump_file"
    assert_success

    run kill $pid
}
