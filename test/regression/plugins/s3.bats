#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

name=$(unix_nano)
CEDANA_S3_BUCKET_NAME="cedana-test-$name"
CEDANA_S3_MANAGED="true"

export CEDANA_S3_BUCKET_NAME
export CEDANA_S3_MANAGED

setup_file() {
    setup_file_daemon
}

setup() {
    setup_daemon
    echo "SETUP_FILE CALLED with bucket: $CEDANA_S3_BUCKET_NAME"
    aws_setup "$CEDANA_S3_BUCKET_NAME"
}

teardown() {
    teardown_daemon
    echo "TEARDOWN_FILE CALLED with bucket: $CEDANA_S3_BUCKET_NAME"
    aws_cleanup_bucket "$CEDANA_S3_BUCKET_NAME"
}

teardown_file() {
    teardown_file_daemon
}

############
### Dump ###
############

@test "stream to s3 dump process (custom name)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --stream 1 --compression none
    assert_success

    assert_exists_s3 "$name/img-0"

    run kill $pid
}

@test "stream to s3 dump process (4 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid  --name "$name" --stream 4 --compression none
    assert_success

    assert_exists_s3 "$name/img-0"
    assert_exists_s3 "$name/img-1"
    assert_exists_s3 "$name/img-2"
    assert_exists_s3 "$name/img-3"

    run kill $pid
}

@test "stream to s3 dump process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
S    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --stream 2 --compression tar
    assert_success

    assert_exists_s3 "$name/img-0"
    assert_exists_s3 "$name/img-1"

    run kill $pid
}

@test "stream to s3 dump process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --stream 2 --compression gzip
    assert_success

    assert_exists_s3 "$name/img-0.gz"
    assert_exists_s3 "$name/img-1.gz"

    run kill $pid
}

@test "stream to s3 dump process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --stream 2 --compression lz4
    assert_success

    assert_exists_s3 "$name/img-0.lz4"
    assert_exists_s3 "$name/img-1.lz4"

    run kill $pid
}


###############
### Restore ###
###############

@test "stream to s3 restore process" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --stream 1 --compression none
    assert_success

    assert_exists_s3 "$name/img-0"

    run cedana restore process --path "s3://$CEDANA_S3_BUCKET_NAME/$name" --stream 1
    assert_success

    run kill $pid
}

@test "stream to s3 restore process (4 parallelism)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --stream 4 --compression none
    assert_success

    assert_exists_s3 "$name/img-0"
    assert_exists_s3 "$name/img-1"
    assert_exists_s3 "$name/img-2"
    assert_exists_s3 "$name/img-3"

    run cedana restore process --path "s3://$CEDANA_S3_BUCKET_NAME/$name" --stream 4
    assert_success

    run kill $pid
}
