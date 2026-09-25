#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=storage:csx

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/csx

load_lib support
load_lib assert
load_lib file

export CEDANA_LOG_LEVEL=trace

setup_file() {
    setup_file_csx_daemon
    setup_file_daemon
}

setup() {
    setup_csx_daemon
    setup_daemon
}

teardown() {
    teardown_daemon
    teardown_csx_daemon
}

teardown_file() {
    teardown_file_csx_daemon
    teardown_file_daemon
}

############
### Dump ###
############

# bats test_tags=dump
@test "(CSX) dump process (new job)" {
    jid=$(unix_nano)

    cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"

    sleep 1

    cedana dump job "$jid" --dir csx://

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "(CSX) dump process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression tar --dir csx://

    run kill $pid
}

# bats test_tags=dump
@test "(CSX) dump process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression gzip --dir csx://

    run kill $pid
}

# bats test_tags=dump
@test "(CSX) dump process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression lz4 --dir csx://

    run kill $pid
}

# bats test_tags=dump
@test "(CSX) dump process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression zlib --dir csx://

    run kill $pid
}

# bats test_tags=dump
@test "(CSX) dump process (no compression, leave running)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)
    name2=$(unix_nano)

    cedana dump process $pid --name "$name" --dir csx:// --compression none --leave-running

    pid_exists $pid

    sleep 1

    cedana dump process $pid --name "$name2" --dir csx:// --compression none

    run kill $pid
}

# bats test_tags=dump
@test "(CSX) dump process (gzip compression, leave running)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)
    name2=$(unix_nano)

    cedana dump process $pid --name "$name" --dir csx:// --compression gzip --leave-running

    pid_exists $pid

    sleep 1

    cedana dump process $pid --name "$name2" --dir csx:// --compression gzip

    run kill $pid
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "(CSX) restore process (new job)" {
    jid=$(unix_nano)

    cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"

    sleep 1

    cedana dump job "$jid" --dir csx://

    cedana restore job "$jid"

    run cedana job kill "$jid"
}

# bats test_tags=restore,bhavik
@test "(CSX) restore process (new job, without daemon)" {
    jid=$(unix_nano)
    code=42

    cedana run process "$WORKLOADS/date-loop.sh" 7 $code --jid "$jid"

    sleep 1

    cedana dump job "$jid" --dir csx:// --name "$jid"

    run cedana restore process --path "csx://$jid" --no-server
    assert_equal $status $code
}

# bats test_tags=restore
@test "(CSX) restore process (tar compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression tar --dir csx://

    cedana restore process --path "csx://$name.tar"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "(CSX) restore process (gzip compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression gzip --dir csx://

    cedana restore process --path "csx://$name.tar.gz"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "(CSX) restore process (lz4 compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression lz4 --dir csx://

    cedana restore process --path "csx://$name.tar.lz4"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}

# bats test_tags=restore
@test "(CSX) restore process (zlib compression)" {
    "$WORKLOADS"/date-loop.sh &
    pid=$!
    name=$(unix_nano)

    sleep 1

    cedana dump process $pid --name "$name" --compression zlib --dir csx://

    cedana restore process --path "csx://$name.tar.zlib"

    run ps --pid $pid
    assert_success
    assert_output --partial "$pid"

    run kill $pid
}
