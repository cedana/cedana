#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=base,run

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

@test "run process" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    cedana run process echo hello --jid "$jid"

    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"

    run cedana ps
    assert_success
    assert_output --partial "$jid"
}

# bats test_tags=serverless
@test "run process (without daemon)" {
    run cedana run --no-server process echo hello
    assert_success
    assert_output --partial "hello"
}

# bats test_tags=serverless
@test "run process (without daemon, exit code)" {
    code=42

    run cedana run --no-server process "$WORKLOADS"/exit-code.sh "$code"
    assert_equal $status $code
}

# bats test_tags=serverless
@test "run process (without daemon, PID file)" {
    pid_file=/tmp/$(unix_nano).pid

    run cedana run --no-server process echo hello --pid-file "$pid_file"
    assert_success
    assert_output --partial "hello"

    assert_exists "$pid_file"
}

@test "run non-existent process" {
    jid=$(unix_nano)

    run cedana run process non-existent --jid "$jid"
    assert_failure

    run cedana ps
    assert_success
    refute_output --partial "$jid"
}

@test "run process with custom log" {
    jid=$(unix_nano)
    log_file="/tmp/$jid.log"

    cedana run process echo hello --jid "$jid" --out "$log_file"

    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"
}

@test "exec (run process alias)" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    cedana exec echo hello --jid "$jid"

    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"
}

# bats test_tags=attach
@test "attach" {
    jid=$(unix_nano)

    run cedana run process echo hello --jid "$jid" --attach
    assert_success
    assert_output --partial "hello"
}

# bats test_tags=attach
@test "attach (exit code)" {
    jid=$(unix_nano)
    code=42

    run cedana run process "$WORKLOADS"/exit-code.sh "$code" --jid "$jid" --attach
    assert_equal $status $code
}

# bats test_tags=attach
@test "attach (using PID)" {
    jid=$(unix_nano)
    code=42

    cedana run process "$WORKLOADS"/date-loop.sh 3 "$code" --jid "$jid" --attachable

    pid=$(pid_for_jid "$jid")

    run cedana attach "$pid"
    assert_equal $status $code
}

# bats test_tags=attach
@test "attach (job)" {
    jid=$(unix_nano)
    code=42

    cedana run process "$WORKLOADS"/date-loop.sh 3 "$code" --jid "$jid" --attachable

    run cedana job attach "$jid"
    assert_equal $status $code
}
