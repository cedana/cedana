#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=containerd

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/containerd

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

    cedana run containerd --image "$image" --jid "$jid" -- "$jid" echo hello

    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"

    run cedana ps
    assert_success
    assert_output --partial "$jid"
}

# bats test_tags=attach
@test "run container with attach" {
    jid=$(unix_nano)
    image="docker.io/library/alpine:latest"

    run cedana run containerd --image "$image" --jid "$jid" --attach -- "$jid" echo hello
    assert_success
    assert_output --partial "hello"
}

# bats test_tags=attach
@test "run container with attach (exit code)" {
    jid=$(unix_nano)
    code=42
    image="docker.io/library/alpine:latest"

    run cedana run containerd --image "$image" --jid "$jid" --attach -- "$jid" sh -c "exit $code"
    assert_equal $status $code
}

# bats test_tags=manage
@test "manage container (existing)" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"

    ctr run --detach "$image" "$id"

    sleep 1

    cedana manage containerd "$id" --jid "$id"

    run cedana ps
    assert_success
    assert_output --partial "$id"

    run cedana job kill "$id"
}

# bats test_tags=manage
@test "manage container (upcoming)" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"

    run cedana manage containerd "$id" --jid "$id" --upcoming &

    sleep 1

    ctr run --detach "$image" "$id"

    sleep 1

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
    image="docker.io/library/nginx:latest"

    ctr run "$image" "$id" &

    sleep 2

    run cedana dump containerd "$id"
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run ctr task kill "$id"
}

# bats test_tags=dump
@test "dump container (detached)" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"

    ctr run --detach "$image" "$id"

    sleep 2

    run cedana dump containerd "$id"
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run ctr task kill "$id"
}

# bats test_tags=dump
@test "dump container (new job)" {
    jid=$(unix_nano)
    image="docker.io/library/nginx:latest"
    new_image="docker.io/library/nginx:$jid"

    cedana run containerd --image "$image" --jid "$jid"

    sleep 3

    run cedana dump job "$jid" --image "$new_image"
    assert_success
    dump_file=$(echo "$output" | tail -n 1  | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana kill "$jid"
}

# bats test_tags=dump
@test "dump container (new job, attached)" {
    jid=$(unix_nano)
    image="docker.io/library/nginx:latest"
    new_image="docker.io/library/nginx:$jid"

    cedana run containerd --image "$image" --jid "$jid" --attach &

    sleep 3

    run cedana dump job "$jid" --image "$new_image"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana kill "$jid"
}

# bats test_tags=dump
@test "dump container (manage existing)" {
    id=$(unix_nano)
    jid="$id"
    image="docker.io/library/nginx:latest"
    new_image="docker.io/library/nginx:$jid"

    ctr run --detach "$image" "$id"

    cedana manage containerd "$id" --jid "$jid"

    sleep 2

    run cedana dump job "$jid" --image "$new_image"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana kill "$jid"
}

# bats test_tags=dump
@test "dump container (manage upcoming)" {
    id=$(unix_nano)
    jid="$id"
    image="docker.io/library/nginx:latest"
    new_image="docker.io/library/nginx:$jid"

    run cedana manage containerd "$id" --jid "$jid" --upcoming &

    sleep 1

    ctr run --detach "$image" "$id"

    sleep 2

    run cedana dump job "$jid" --image "$new_image"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana kill "$jid"
}
