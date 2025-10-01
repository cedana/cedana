#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=remote,storage:s3

load ../../helpers/utils
load ../../helpers/daemon

load_lib support
load_lib assert
load_lib file

setup_file() {
    if ! env_exists AWS_ACCESS_KEY_ID || ! env_exists AWS_SECRET_ACCESS_KEY; then
        skip "AWS credentials not set"
    fi
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
@test "remote (S3) dump process (new job)" {
    jid=$(unix_nano)

    cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"

    sleep 1

    cedana dump job "$jid" --dir s3://checkpoints-ci

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "remote (S3) dump process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression tar --dir s3://checkpoints-ci

    run kill $pid
}

# bats test_tags=dump
@test "remote (S3) dump process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression gzip --dir s3://checkpoints-ci

    run kill $pid
}

# bats test_tags=dump
@test "remote (S3) dump process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression lz4 --dir s3://checkpoints-ci

    run kill $pid
}

# bats test_tags=dump
@test "remote (S3) dump process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression zlib --dir s3://checkpoints-ci

    run kill $pid
}

# bats test_tags=dump
@test "remote (S3) dump process (no compression, leave running)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)
    name2=$(unix_nano)

    cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --compression none --leave-running

    pid_exists $pid

    sleep 1

    cedana dump process $pid --name "$name2" --dir s3://checkpoints-ci --compression none

    run kill $pid
}

# bats test_tags=dump
@test "remote (S3) dump process (gzip compression, leave running)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)
    name2=$(unix_nano)

    cedana dump process $pid --name "$name" --dir s3://checkpoints-ci --compression gzip --leave-running

    pid_exists $pid

    sleep 1

    cedana dump process $pid --name "$name2" --dir s3://checkpoints-ci --compression gzip

    run kill $pid
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "remote (S3) restore process (new job)" {
    jid=$(unix_nano)

    cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"

    sleep 1

    cedana dump job "$jid" --dir s3://checkpoints-ci

    cedana restore job "$jid"

    run cedana job kill "$jid"
}

# bats test_tags=restore
@test "remote (S3) restore process (new job, without daemon)" {
    jid=$(unix_nano)
    code=42

    cedana run process "$WORKLOADS/date-loop.sh" 7 $code --jid "$jid"

    sleep 1

    cedana dump job "$jid" --dir s3://checkpoints-ci --name "$jid"

    run cedana restore process --path "s3://checkpoints-ci/$jid.tar" --no-server
    assert_equal $status $code
}

# bats test_tags=restore
@test "remote (S3) restore process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression tar --dir s3://checkpoints-ci

    cedana restore process --path "s3://checkpoints-ci/$name.tar"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "remote (S3) restore process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression gzip --dir s3://checkpoints-ci

    cedana restore process --path "s3://checkpoints-ci/$name.tar.gz"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "remote (S3) restore process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression lz4 --dir s3://checkpoints-ci

    cedana restore process --path "s3://checkpoints-ci/$name.tar.lz4"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "remote (S3) restore process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression zlib --dir s3://checkpoints-ci

    cedana restore process --path "s3://checkpoints-ci/$name.tar.zlib"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}
