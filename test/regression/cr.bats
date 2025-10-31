#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=base,cr

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
@test "dump process" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!

    run cedana dump process $pid
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run kill $pid
}

# bats test_tags=dump
@test "dump process (custom name)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression none

    assert_exists "/tmp/$name"

    run kill $pid
}

# bats test_tags=dump
@test "dump process (custom dir)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    mkdir -p /tmp/"$name"

    cedana dump process $pid --name "$name" --dir /tmp/"$name" --compression none

    assert_exists "/tmp/$name/$name"

    run kill $pid
}

# bats test_tags=dump
@test "dump process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression tar

    assert_exists "/tmp/$name.tar"

    run kill $pid
}

# bats test_tags=dump
@test "dump process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression gzip

    assert_exists "/tmp/$name.tar.gz"

    run kill $pid
}

# bats test_tags=dump
@test "dump process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression lz4

    assert_exists "/tmp/$name.tar.lz4"

    run kill $pid
}

# bats test_tags=dump
@test "dump process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression zlib

    assert_exists "/tmp/$name.tar.zlib"

    run kill $pid
}

# bats test_tags=dump
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

# bats test_tags=dump
@test "dump process (no compression, leave running)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)
    name2=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression none --leave-running

    pid_exists $pid
    assert_exists "/tmp/$name"

    sleep 1

    cedana dump process $pid --name "$name2" --dir /tmp --compression none

    assert_exists "/tmp/$name2"

    run kill $pid
}

# bats test_tags=dump
@test "dump process (gzip compression, leave running)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)
    name2=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression gzip --leave-running

    pid_exists $pid
    assert_exists "/tmp/$name.tar.gz"

    sleep 1

    cedana dump process $pid --name "$name2" --dir /tmp --compression gzip

    assert_exists "/tmp/$name2.tar.gz"

    run kill $pid
}

# bats test_tags=dump
@test "dump process (new job)" {
    jid=$(unix_nano)

    cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"
}

# bats test_tags=dump,manage
@test "dump process (manage existing job)" {
    jid=$(unix_nano)

    "$WORKLOADS"/date-loop.sh &
    pid=$!

    cedana manage process $pid --jid "$jid"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "dump non-existent process" {
    id=$(unix_nano)
    mkdir -p /tmp/dump-"$id"

    run cedana dump process 999999999 --dir /tmp
    assert_failure

    # check that there are no files inside the dump dir
    [[ -z $(ls /tmp/dump-"$id") ]]

    rm -rf /tmp/dump-"$id"
}

# bats test_tags=dump
@test "dump non-existent job" {
    run cedana dump job 999999999
    assert_failure
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "restore process" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!

    run cedana dump process $pid
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore process --path "$dump_file"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "restore process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression tar

    assert_exists "/tmp/$name.tar"

    cedana restore process --path "/tmp/$name.tar"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "restore process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression gzip

    assert_exists "/tmp/$name.tar.gz"

    cedana restore process --path "/tmp/$name.tar.gz"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "restore process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression lz4

    assert_exists "/tmp/$name.tar.lz4"

    cedana restore process --path "/tmp/$name.tar.lz4"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "restore process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression zlib

    assert_exists "/tmp/$name.tar.zlib"

    cedana restore process --path "/tmp/$name.tar.zlib"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "restore process (compression invalid)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression tar

    mv "/tmp/$name.tar" "/tmp/$name.tar.jibberish"

    run cedana restore process --path "/tmp/$name.tar.jibberish"
    assert_failure

    run kill $pid
}

# bats test_tags=restore
@test "restore process (new job)" {
    jid=$(unix_nano)

    cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
}

# bats test_tags=restore,manage
@test "restore process (manage existing job)" {
    jid=$(unix_nano)
    log="/tmp/restore-$jid.log"

    "$WORKLOADS"/date-loop.sh &> "$log" < /dev/null &
    pid=$!

    cedana manage process $pid --jid "$jid"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
}

@test "restore process (PID file)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)
    pid_file="/tmp/$name.pid"

    cedana dump process $pid --name "$name" --dir /tmp --compression tar

    cedana restore process --path "/tmp/$name.tar" --pid-file "$pid_file"

    assert_exists "$pid_file"

    run kill $pid
}

# bats test_tags=restore,daemonless
@test "restore process (without daemon)" {
    "$WORKLOADS"/date-loop.sh 3 &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression tar

    cedana restore process --path "/tmp/$name.tar" --no-server
}

# bats test_tags=restore,daemonless
@test "restore process (without daemon, exit code)" {
    code=42
    "$WORKLOADS"/date-loop.sh 3 "$code" &
    pid=$!
    name=$(unix_nano)

    cedana dump process $pid --name "$name" --dir /tmp --compression tar

    run cedana restore process --path "/tmp/$name.tar" --no-server
    assert_equal $status $code
}

# bats test_tags=restore,daemonless
@test "restore process (without daemon, PID file)" {
    "$WORKLOADS"/date-loop.sh 3 &
    pid=$!
    name=$(unix_nano)
    pid_file="/tmp/$name.pid"

    cedana dump process $pid --name "$name" --dir /tmp --compression tar

    cedana restore process --path "/tmp/$name.tar" --no-server --pid-file "$pid_file"

    assert_exists "$pid_file"
}

# bats test_tags=restore
@test "restore process (job to without daemon)" {
    code=42
    jid=$(unix_nano)

    cedana run process "$WORKLOADS/date-loop.sh" 3 "$code" --jid "$jid"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore process --path "$dump_file" --no-server
    assert_equal $status $code
}

# bats test_tags=restore
@test "restore attach (non-attached to attached)" {
    code=42
    jid=$(unix_nano)

    cedana run process "$WORKLOADS/date-loop.sh" 3 "$code" --jid "$jid"

    cedana dump job "$jid"

    run cedana restore job "$jid" --attach
    assert_equal $status $code
}

# bats test_tags=restore
@test "restore attach (attached to attached)" {
    code=42
    jid=$(unix_nano)

    cedana run process "$WORKLOADS/date-loop.sh" 3 "$code" --jid "$jid" --attachable

    cedana dump job "$jid"

    run cedana restore job "$jid" --attach
    assert_equal $status $code
}

# bats test_tags=restore
@test "restore attach (attached to non-attached)" {
    code=42
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    cedana run process "$WORKLOADS/date-loop.sh" 3 "$code" --jid "$jid" --attachable

    cedana dump job "$jid"

    cedana restore job "$jid"

    sleep 3
    assert_file_contains "$log_file" "$code"
}

# bats test_tags=restore
@test "restore attach (attached to without daemon)" {
    code=42
    jid=$(unix_nano)

    cedana run process "$WORKLOADS/date-loop.sh" 3 "$code" --jid "$jid" --attachable

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore process --path "$dump_file" --no-server
    assert_equal $status $code
}

# bats test_tags=restore
@test "restore non-existent process" {
    run cedana restore process --path /tmp/non-existent
    assert_failure

    run ps --pid 999999999
    assert_failure
}

# bats test_tags=restore
@test "restore non-existent job" {
    run cedana restore job 999999999
    assert_failure
}
