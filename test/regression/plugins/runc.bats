#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=runc

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

###########
### Run ###
###########

@test "run container" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"
    bundle="$(create_cmd_bundle "echo hello")"

    cedana run runc --bundle "$bundle" --jid "$jid"

    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"

    run cedana ps
    assert_success
    assert_output --partial "$jid"
}

# bats test_tags=serverless
@test "run container (without daemon)" {
    jid=$(unix_nano)
    bundle="$(create_cmd_bundle "echo hello")"

    run cedana run --no-server runc --bundle "$bundle" --jid "$jid"
    assert_success
    assert_output --partial "hello"

    run runc delete "$jid"
}

# bats test_tags=serverless
@test "run container (without daemon, exit code)" {
    jid=$(unix_nano)
    code=42
    bundle="$(create_workload_bundle "exit-code.sh" "$code")"

    run cedana run --no-server runc --bundle "$bundle" --jid "$jid"
    assert_equal $status $code

    run runc delete "$jid"
}

# bats test_tags=serverless
@test "run container (without daemon, detached)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    cedana run runc --no-server --bundle "$bundle" --detach --jid "$jid"

    sleep 1

    assert_equal "$(container_status "$jid")" "running"

    run runc kill "$jid" KILL
    run runc delete "$jid"
}

# bats test_tags=serverless
@test "run container (without daemon, PID file)" {
    jid=$(unix_nano)
    bundle="$(create_cmd_bundle "echo hello")"
    pid_file="/tmp/$jid.pid"

    run cedana run --no-server runc --bundle "$bundle" --jid "$jid" --pid-file "$pid_file"
    assert_success
    assert_output --partial "hello"

    assert_exists "$pid_file"

    run runc delete "$jid"
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

    cedana run runc --bundle "$bundle" --jid "$jid" --out "$log_file"

    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"
}

# bats test_tags=attach
@test "run container with attach" {
    jid=$(unix_nano)
    bundle="$(create_cmd_bundle "echo hello")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --attach
    assert_success
    assert_output --partial "hello"
}

# bats test_tags=attach
@test "run container with attach (exit code)" {
    jid=$(unix_nano)
    code=42
    bundle="$(create_workload_bundle "exit-code.sh" "$code")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --attach
    assert_equal $status $code
}

# bats test_tags=manage
@test "manage container (existing)" {
    id=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --detach --bundle "$bundle" "$id"

    sleep 1

    cedana manage runc "$id" --bundle "$bundle" --jid "$id"

    run cedana ps
    assert_success
    assert_output --partial "$id"

    run cedana job kill "$id"
}

# bats test_tags=manage
@test "manage container (upcoming)" {
    id=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    run cedana manage runc "$id" --bundle "$bundle" --jid "$id" --upcoming &

    sleep 2

    runc run --detach --bundle "$bundle" "$id"

    sleep 2

    run cedana ps
    assert_success
    assert_output --partial "$id"

    run cedana job kill "$id"
}

############
### Dump ###
############

# bats test_tags=dump
@test "dump container" {
    id=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    run cedana dump runc "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=dump
@test "dump container (detached)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --bundle "$bundle" "$jid" --detach

    run cedana dump runc "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$jid" KILL
    run runc delete "$jid"
}

# bats test_tags=dump
@test "dump container (new job)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    cedana run runc --bundle "$bundle" --jid "$jid"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana kill "$jid"
}

# bats test_tags=dump
@test "dump container (new job, attached)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"
    pid_file="/tmp/$(unix_nano).pid"

    cedana run runc --bundle "$bundle" --jid "$jid" --pid-file "$pid_file" --attach &

    wait_for_file "$pid_file"
    sleep 1

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana kill "$jid"
}

# bats test_tags=dump,manage
@test "dump container (manage existing)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=dump,manage
@test "dump container (manage upcoming)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    run cedana manage runc "$id" --jid "$jid" --bundle "$bundle" --upcoming &

    sleep 2

    runc run --detach --bundle "$bundle" "$id"

    sleep 2

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=dump
@test "dump container (external NET namespace)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    share_namespace "$bundle" "network" "/proc/1/ns/net"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=dump
@test "dump container (external PID namespace)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    share_namespace "$bundle" "pid" "/proc/1/ns/pid"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=dump
@test "dump container (external IPC namespace)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    share_namespace "$bundle" "ipc" "/proc/1/ns/ipc"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=dump
@test "dump container (external UTS namespace)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    share_namespace "$bundle" "uts" "/proc/1/ns/uts"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=dump
@test "dump container (external CGROUP namespace)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    share_namespace "$bundle" "cgroup" "/proc/1/ns/cgroup"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=dump
@test "dump container (external ALL namespace)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    share_namespace "$bundle" "network" "/proc/1/ns/net"
    share_namespace "$bundle" "pid" "/proc/1/ns/pid"
    share_namespace "$bundle" "ipc" "/proc/1/ns/ipc"
    share_namespace "$bundle" "uts" "/proc/1/ns/uts"
    share_namespace "$bundle" "cgroup" "/proc/1/ns/cgroup"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=dump
@test "dump container (external binds mount)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    add_bind_mount "$bundle" "$(mktemp -d)" "/random/path/to/dir"
    add_bind_mount "$bundle" "$(mktemp -d)" "/random/path/to/dir2"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=dump
@test "dump container (external binds mount and namespaces)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    add_bind_mount "$bundle" "$(mktemp -d)" "/random/path/to/dir"
    add_bind_mount "$bundle" "$(mktemp -d)" "/random/path/to/dir2"
    share_namespace "$bundle" "network" "/proc/1/ns/net"
    share_namespace "$bundle" "pid" "/proc/1/ns/pid"
    share_namespace "$bundle" "ipc" "/proc/1/ns/ipc"
    share_namespace "$bundle" "uts" "/proc/1/ns/uts"
    share_namespace "$bundle" "cgroup" "/proc/1/ns/cgroup"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "restore container" {
    id=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    run cedana dump runc "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore runc --id "$id" --path "$dump_file" --bundle "$bundle"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore
@test "restore container (PID file)" {
    id=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"
    pid_file="/tmp/$id.pidfile"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    run cedana dump runc "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore runc --id "$id" --path "$dump_file" --bundle "$bundle" --pid-file "$pid_file"

    assert_exists "$pid_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore,serverless
@test "restore container (without daemon)" {
    id=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh" 3)"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    run cedana dump runc "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore runc --id "$id" --path "$dump_file" --bundle "$bundle" --no-server

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore,serverless
@test "restore container (without daemon, exit code)" {
    id=$(unix_nano)
    code=42
    bundle="$(create_workload_bundle "date-loop.sh" 3 "$code")"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    run cedana dump runc "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore runc --id "$id" --path "$dump_file" --bundle "$bundle" --no-server
    assert_equal $status $code

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore,serverless
@test "restore container (without daemon, PID file)" {
    id=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh" 3)"
    pid_file="/tmp/$id.pidfile"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    run cedana dump runc "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore runc --id "$id" --path "$dump_file" --bundle "$bundle" --no-server --pid-file "$pid_file"

    assert_exists "$pid_file"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore
@test "restore container (detached to attached)" {
    id=$(unix_nano)
    code=42
    bundle="$(create_workload_bundle "date-loop.sh" 3 "$code")"

    runc run --bundle "$bundle" "$id" --detach > /dev/null 2>&1 < /dev/null

    run cedana dump runc "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc delete "$id"

    run cedana restore runc --id "$id" --path "$dump_file" --bundle "$bundle" --attach
    assert_equal $status $code

    status=$(container_status "$id")
    assert_equal "$status" "" # should be automatically deleted (as it was attached)
}

# bats test_tags=restore,serverless
@test "restore container (detached to without daemon)" {
    id=$(unix_nano)
    code=42
    bundle="$(create_workload_bundle "date-loop.sh" 3 "$code")"

    runc run --bundle "$bundle" "$id" --detach > /dev/null 2>&1 < /dev/null

    run cedana dump runc "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc delete "$id"

    run cedana restore runc --id "$id" --path "$dump_file" --bundle "$bundle" --no-server
    assert_equal $status $code

    status=$(container_status "$id")
    assert_equal "$status" "" # should be automatically deleted (as not detached & without daemon)
}

# bats test_tags=restore,serverless
@test "restore container (detached to without daemon detached)" {
    id=$(unix_nano)
    code=42
    bundle="$(create_workload_bundle "date-loop.sh" 3 "$code")"

    runc run --bundle "$bundle" "$id" --detach > /dev/null 2>&1 < /dev/null

    run cedana dump runc "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run runc delete "$id"

    cedana restore runc --id "$id" --path "$dump_file" --bundle "$bundle" --no-server --detach

    status=$(container_status "$id")
    assert_equal "$status" "running"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore
@test "restore container (new job)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    cedana run runc --bundle "$bundle" --jid "$jid"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run cedana kill "$jid"
}

# bats test_tags=restore
@test "restore container (new job, attached)" {
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"
    pid_file="/tmp/$(unix_nano).pid"

    cedana run runc --bundle "$bundle" --jid "$jid" --pid-file "$pid_file" --attach &

    wait_for_file "$pid_file"
    sleep 1

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run cedana kill "$jid"
}

# bats test_tags=restore
@test "restore container (manage existing job)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore
@test "restore container (external NET namespace)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    share_namespace "$bundle" "network" "/proc/1/ns/net"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore
@test "restore container (external PID namespace)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    share_namespace "$bundle" "pid" "/proc/1/ns/pid"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore
@test "restore container (external IPC namespace)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    share_namespace "$bundle" "ipc" "/proc/1/ns/ipc"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore
@test "restore container (external UTS namespace)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    share_namespace "$bundle" "uts" "/proc/1/ns/uts"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore
@test "restore container (external CGROUP namespace)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    share_namespace "$bundle" "cgroup" "/proc/1/ns/cgroup"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana restore job "$jid"
    assert_failure
    assert_output --partial "CRIU does not support joining cgroup namespace"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore
@test "restore container (external ALL namespace)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    share_namespace "$bundle" "network" "/proc/1/ns/net"
    share_namespace "$bundle" "pid" "/proc/1/ns/pid"
    share_namespace "$bundle" "ipc" "/proc/1/ns/ipc"
    share_namespace "$bundle" "uts" "/proc/1/ns/uts"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore
@test "restore container (external bind mounts)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    add_bind_mount "$bundle" "$(mktemp -d)" "/random/path/to/dir"
    add_bind_mount "$bundle" "$(mktemp -d)" "/random/path/to/dir2"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run runc kill "$id" KILL
    run runc delete "$id"
}

# bats test_tags=restore
@test "restore container (external bind mounts and namespaces)" {
    id=$(unix_nano)
    jid=$(unix_nano)
    bundle="$(create_workload_bundle "date-loop.sh")"

    add_bind_mount "$bundle" "$(mktemp -d)" "/random/path/to/dir"
    add_bind_mount "$bundle" "$(mktemp -d)" "/random/path/to/dir2"
    share_namespace "$bundle" "network" "/proc/1/ns/net"
    share_namespace "$bundle" "pid" "/proc/1/ns/pid"
    share_namespace "$bundle" "ipc" "/proc/1/ns/ipc"
    share_namespace "$bundle" "uts" "/proc/1/ns/uts"

    runc run --bundle "$bundle" "$id" &

    sleep 2

    cedana manage runc "$id" --jid "$jid" --bundle "$bundle"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run runc kill "$id" KILL
    run runc delete "$id"
}

@test "run container (persistent mounts)" {
    jid=$(unix_nano)
    bundle="$(create_cmd_bundle "while true; do date > /persistent/date.txt; sleep 1; done")"

    add_env_var "$bundle" "CEDANA_PERSISTENT_MOUNTS" "/persistent"

    cedana run runc --bundle "$bundle" --jid "$jid"

    sleep 2

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid"

    run cedana kill "$jid"
}
