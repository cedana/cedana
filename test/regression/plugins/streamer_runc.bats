#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=streamer,runc

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/runc

load_lib support
load_lib assert
load_lib file

setup_file() {
    setup_file_daemon
    do_once setup_rootfs
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
@test "stream dump container" {
    id=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --bundle "$bundle" "$id" &

    sleep 1

    run cedana dump runc "$id" --streams 1 --compression none
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=dump
@test "stream dump container (detached)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --bundle "$bundle" "$jid" --detach

    run cedana dump runc "$jid" --streams 2 --compression none
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"
    assert_exists "$dump_file/img-1"

    run runc kill "$jid" KILL
    run runc delete "$jid"
}

# bats test_tags=dump
@test "stream dump container (new job)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    cedana run runc --bundle "$bundle" --jid "$jid"

    run cedana dump job "$jid" --streams 2 --compression none
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"
    assert_exists "$dump_file/img-1"

    run cedana kill "$jid"
}

# bats test_tags=dump
@test "stream dump container (new job, attached)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    cedana run runc --bundle "$bundle" --jid "$jid" --attach &

    sleep 1

    run cedana dump job "$jid" --streams 2 --compression none
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"
    assert_exists "$dump_file/img-1"

    run cedana kill "$jid"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "stream restore container" {
    id=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --bundle "$bundle" "$id" &

    sleep 1

    run cedana dump runc "$id" --streams 1 --compression none
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"

    cedana restore runc --id "$id" --path "$dump_file" --bundle "$bundle"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore
@test "stream restore container (new job)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    cedana run runc --bundle "$bundle" --jid "$jid"

    run cedana dump job "$jid" --streams 2 --compression none
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"
    assert_exists "$dump_file/img-1"

    cedana restore job "$jid"

    run cedana kill "$jid"
}

# bats test_tags=restore
@test "restore container (new job, attached)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    cedana run runc --bundle "$bundle" --jid "$jid" --attach &
    sleep 1

    run cedana dump job "$jid" --streams 2 --compression none
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0"
    assert_exists "$dump_file/img-1"

    cedana restore job "$jid"

    run cedana kill "$jid"
}

# bats test_tags=restore
@test "restore container (manage existing job)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --bundle "$bundle" "$id" &

    sleep 1

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run runc kill "$id" KILL
    run runc delete "$id"
}
