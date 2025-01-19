#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile

load helpers/utils
load helpers/daemon

load_lib support
load_lib assert
load_lib file

@test "run process" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana run process echo hello --jid "$jid"

    assert_success
    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"

    run cedana ps

    assert_success
    assert_output --partial "$jid"
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

    run cedana run process echo hello --jid "$jid" --log "$log_file"

    assert_success
    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"
}

@test "exec (run process alias)" {
    jid=$(unix_nano)

    run cedana exec echo hello --jid "$jid"

    log_file="/var/log/cedana-output-$jid.log"

    assert_success
    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"
}

@test "run process with attach" {
    jid=$(unix_nano)

    run cedana run process echo hello --jid "$jid" --attach

    assert_success
    assert_output --partial "hello"
}

@test "run process with attach (exit code)" {
    jid=$(unix_nano)
    code=42

    run cedana run process "$WORKLOADS"/exit-code.sh "$code" --jid "$jid" --attach

    assert_equal $status $code
}

@test "attach (using PID)" {
    jid=$(unix_nano)
    code=42

    cedana run process "$WORKLOADS"/date-loop.sh 3 "$code" --jid "$jid" --attachable

    pid=$(pid_for_jid "$jid")

    run cedana attach "$pid"

    assert_equal $status $code
}

@test "attach job" {
    jid=$(unix_nano)
    code=42

    cedana run process "$WORKLOADS"/date-loop.sh 3 "$code" --jid "$jid" --attachable

    run cedana job attach "$jid"

    assert_equal $status $code
}
