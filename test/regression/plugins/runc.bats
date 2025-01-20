#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile

load ../helpers/utils
load ../helpers/daemon
load ../helpers/runc

load_lib support
load_lib assert
load_lib file

###########
### Run ###
###########

@test "run container" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"
    bundle="$(create_cmd_bundle "echo hello")"

    run cedana run runc --bundle "$bundle" --jid "$jid"

    assert_success
    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"

    run cedana ps

    assert_success
    assert_output --partial "$jid"
}

@test "run non-existent container" {
    jid=$(unix_nano)

    run cedana run runc --bundle "/non-existent" --jid "$jid"

    assert_failure

    run cedana ps

    assert_success
    refute_output --partial "$jid"
}

@test "run container with custom log" {
    jid=$(unix_nano)
    log_file="/tmp/$jid.log"
    bundle="$(create_cmd_bundle "echo hello")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --log "$log_file"

    assert_success
    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"
}

@test "run container with attach" {
    jid=$(unix_nano)
    bundle="$(create_cmd_bundle "echo hello")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --attach

    assert_success
    assert_output --partial "hello"
}

@test "run container with attach (exit code)" {
    jid=$(unix_nano)
    code=42
    bundle="$(create_workload_bundle "exit-code.sh" "$code")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --attach

    assert_equal $status $code
}

############
### Dump ###
############

@test "dump container" {
    id=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --bundle "$bundle" "$id" &

    sleep 1

    run cedana dump runc "$id"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

@test "dump container (detached)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --bundle "$bundle" "$jid" --detach

    run cedana dump runc "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$jid" KILL
    run runc delete "$jid"
}

@test "dump container (new job)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    run cedana run runc --bundle "$bundle" --jid "$jid"
    assert_success

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana kill "$jid"
}

@test "dump container (new job, attached)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    cedana run runc --bundle "$bundle" --jid "$jid" --attach &
    sleep 1

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana kill "$jid"
}

@test "dump container (manage existing job)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --bundle "$bundle" "$id" &

    sleep 1

    run cedana manage runc "$id" --jid "$jid" --bundle "$bundle"
    assert_success

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

###############
### Restore ###
###############

@test "restore container" {
    id=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --bundle "$bundle" "$id" &

    sleep 1

    run cedana dump runc "$id"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore runc --id "$id" --path "$dump_file" --bundle "$bundle"
    assert_success

    run runc kill "$id" KILL
    run runc delete "$id"
}

# FIXME: Below test fails because when using detach, TTY is inherited
# and CRIU does not know how to restore that.

# @test "restore container (detached)" {
#     id=$(unix_nano)
#     bundle="$(create_workload_bundle "date-loop.sh")"

#     runc run --bundle "$bundle" "$id" --detach

#     run cedana -P "$PORT" dump runc "$id"
#     assert_success

#     dump_file=$(echo "$output" | awk '{print $NF}')
#     assert_exists "$dump_file"

#     run runc delete "$id"

#     run cedana -P "$PORT" restore runc "$id" --path "$dump_file" --bundle "$bundle"
#     assert_success

#     run runc kill "$id" KILL
#     run runc delete "$id"
# }

@test "restore container (new job)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    run cedana run runc --bundle "$bundle" --jid "$jid"
    assert_success

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore job "$jid"
    assert_success

    run cedana kill "$jid"
}

@test "restore container (new job, attached)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    cedana run runc --bundle "$bundle" --jid "$jid" --attach &
    sleep 1

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore job "$jid"
    assert_success

    run cedana kill "$jid"
}
