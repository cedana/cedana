#!/usr/bin/env bats

# The tests here test the remote DB capabilities of the daemon.

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=base,remote

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

export CEDANA_DB_REMOTE=true

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

###########
### Run ###
###########

# bats test_tags=run
@test "run process (remote)" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    cedana run process echo hello --jid "$jid"

    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job delete "$jid"
    assert_output --partial "Deleted"

    run cedana ps
    assert_success
    refute_output --partial "$jid"
}

# bats test_tags=run
@test "run non-existent process (remote)" {
    jid=$(unix_nano)

    run cedana run process non-existent --jid "$jid"
    assert_failure

    run cedana ps
    assert_success
    refute_output --partial "$jid"
}

############
### Dump ###
############

# bats test_tags=dump
@test "dump process (new job, remote)" {
    jid=$(unix_nano)

    cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"

    sleep 1

    run cedana job delete "$jid"
    assert_output --partial "Deleted"

    run cedana ps
    assert_success
    refute_output --partial "$jid"
}

# bats test_tags=dump,manage
@test "dump process (manage existing job, remote)" {
    jid=$(unix_nano)

    "$WORKLOADS"/date-loop.sh &
    pid=$!

    cedana manage process $pid --jid "$jid"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"

    sleep 1

    run cedana job delete "$jid"
    assert_output --partial "Deleted"

    run cedana ps
    assert_success
    refute_output --partial "$jid"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "restore process (new job, remote)" {
    jid=$(unix_nano)

    cedana run process "$WORKLOADS/date-loop.sh" --jid "$jid"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"

    sleep 1

    run cedana job delete "$jid"
    assert_output --partial "Deleted"

    run cedana ps
    assert_success
    refute_output --partial "$jid"
}

# bats test_tags=restore,manage
@test "restore process (manage existing job, remote)" {
    jid=$(unix_nano)
    log="/tmp/restore-$jid.log"

    "$WORKLOADS"/date-loop.sh &> "$log" < /dev/null &
    pid=$!

    cedana manage process $pid --jid "$jid"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"

    sleep 1

    run cedana job delete "$jid"
    assert_output --partial "Deleted"

    run cedana ps
    assert_success
    refute_output --partial "$jid"
}
