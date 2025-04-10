#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

CEDANA_S3_BUCKET_NAME="cedana-test"
CEDANA_S3_MANAGED="true"

export CEDANA_S3_BUCKET_NAME
export CEDANA_S3_MANAGED

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

@test "stream dump process (custom name)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    run cedana dump process $pid --name "$name" --dir /tmp --stream 1 --compression none
    assert_success

    assert_exists_s3 "/tmp/$name/img-0"

    run kill $pid

    aws_cleanup $name
}
