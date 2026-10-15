#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=podman

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/podman

load_lib support
load_lib assert
load_lib file

setup_file() {
    do_once pull_images
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

@test "run container" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"
    image="docker.io/library/alpine:latest"

    cedana run podman --jid "$jid" -- "$image" echo hello

    wait_for_cmd 10 grep -q hello "$log_file"

    run cedana ps
    assert_success
    assert_output --partial "$jid"
}

# bats test_tags=attach
@test "run container with attach" {
    jid=$(unix_nano)
    image="docker.io/library/alpine:latest"

    run cedana run podman --jid "$jid" --attach -- "$image" echo hello
    assert_success
    assert_output --partial "hello"
}

# bats test_tags=attach
@test "run container with attach (exit code)" {
    jid=$(unix_nano)
    code=42
    image="docker.io/library/alpine:latest"

    run cedana run podman --jid "$jid" --attach -- "$image" sh -c "exit $code"
    assert_equal $status $code
}

# bats test_tags=attach
@test "run container with attach (stdin)" {
    jid=$(unix_nano)
    image="docker.io/library/alpine:latest"

    run bash -c "echo hello | cedana run podman --jid $jid --attach -- $image sh -c 'read value; echo got \$value'"
    assert_success
    assert_output --partial "got hello"
}

# bats test_tags=manage
@test "manage container (existing)" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"

    podman run --detach --name "$id" "$image"

    cedana manage podman "$id" --jid "$id"

    run cedana ps
    assert_success
    assert_output --partial "$id"

    run cedana job kill "$id"
}

# bats test_tags=manage
@test "manage container (upcoming)" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"
    pid_file="/tmp/$(unix_nano).pid"

    run cedana manage podman "$id" --jid "$id" --pid-file "$pid_file" --upcoming &

    podman run --detach --name "$id" "$image"

    wait_for_file "$pid_file"

    run cedana ps
    assert_success
    assert_output --partial "$id"

    run cedana job kill "$id"
}

#############
### Query ###
#############

# bats test_tags=query
@test "query container" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"

    podman run --detach --name "$id" "$image"

    run cedana query podman "$id"
    assert_success
    assert_output --partial "$id"
    assert_output --partial "crun"

    run podman rm --force --time 0 "$id"
}

############
### Dump ###
############

# bats test_tags=dump
@test "dump container" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"

    podman run --detach --name "$id" "$image"

    sleep 1

    run cedana dump podman "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run podman rm --force --time 0 "$id"
}

# bats test_tags=dump
@test "dump container (runc runtime)" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"

    podman run --runtime runc --detach --name "$id" "$image"

    sleep 1

    run cedana dump podman "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run podman rm --force --time 0 "$id"
}

# bats test_tags=dump
@test "dump container (cedana runtime)" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"

    podman run --runtime "$(command -v cedana-oci-runtime)" --detach --name "$id" "$image"

    sleep 1

    run cedana dump podman "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run podman rm --force --time 0 "$id"
}

# bats test_tags=dump
@test "dump container (new job)" {
    jid=$(unix_nano)
    image="docker.io/library/nginx:latest"

    cedana run podman --jid "$jid" "$image"

    sleep 2

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "dump container (manage existing)" {
    id=$(unix_nano)
    jid="$id"
    image="docker.io/library/nginx:latest"

    podman run --detach --name "$id" "$image"

    cedana manage podman "$id" --jid "$jid"

    sleep 2

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana job kill "$jid"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "restore container" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"

    podman run --detach --name "$id" "$image"

    sleep 1

    run cedana dump podman "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"
    podman rm --force --time 0 "$id"

    cedana restore podman --path "$dump_file" --id "$id"

    wait_for_container "$id"

    run podman exec "$id" echo hello
    assert_success
    assert_output --partial "hello"

    run podman rm --force --time 0 "$id"
}

# bats test_tags=restore
@test "restore container (runc runtime)" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"

    podman run --runtime runc --detach --name "$id" "$image"

    sleep 1

    run cedana dump podman "$id"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"
    podman rm --force --time 0 "$id"

    cedana restore podman --path "$dump_file" --id "$id"

    wait_for_container "$id"

    run podman inspect --format '{{index .Config.Annotations "cedana.ai/runtime"}}' "$id"
    assert_success
    assert_output "runc"

    run podman rm --force --time 0 "$id"
}

# bats test_tags=restore
@test "restore container (new job)" {
    jid=$(unix_nano)
    image="docker.io/library/nginx:latest"

    cedana run podman --jid "$jid" "$image"

    sleep 2

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    cedana restore job "$jid" --path "$dump_file"

    wait_for_container "$jid"

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
}

# bats test_tags=restore
@test "restore container (state preserved)" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"
    image="docker.io/library/alpine:latest"

    # A counter that survives restore only if the process state does
    cedana run podman --jid "$jid" -- "$image" sh -c 'i=0; while :; do i=$((i+1)); echo $i; sleep 0.1; done'

    wait_for_cmd 10 "[ \$(tail -n 1 $log_file) -ge 20 ]"

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"
    count=$(tail -n 1 "$log_file")

    cedana restore job "$jid" --path "$dump_file"

    wait_for_container "$jid"

    wait_for_cmd 10 "[ \$(podman logs --tail 1 $jid) -gt $count ]"

    run cedana job kill "$jid"
}
