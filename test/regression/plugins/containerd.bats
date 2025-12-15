#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=containerd

# TODO: Diversify images used in tests (currently only nginx)

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

    cedana run containerd --jid "$jid" -- "$image" echo hello

    run cat "$log_file"
    assert_success
    assert_output --partial "hello"

    run cedana ps
    assert_success
    assert_output --partial "$jid"
}

# bats test_tags=attach
@test "run container with attach" {
    jid=$(unix_nano)
    image="docker.io/library/alpine:latest"

    run cedana run containerd --jid "$jid" --attach -- "$image" echo hello
    assert_success
    assert_output --partial "hello"
}

# bats test_tags=attach
@test "run container with attach (exit code)" {
    jid=$(unix_nano)
    code=42
    image="docker.io/library/alpine:latest"

    run cedana run containerd --jid "$jid" --attach -- "$image" sh -c "exit $code"
    assert_equal $status $code
}

# bats test_tags=manage
@test "manage container (existing)" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"
    pid_file="/tmp/$(unix_nano).pid"
    port=$(random_free_port)

    ctr run --detach --pid-file "$pid_file" --env NGINX_PORT="$port" "$image" "$id"

    wait_for_file "$pid_file"

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
    pid_file="/tmp/$(unix_nano).pid"
    port=$(random_free_port)

    run cedana manage containerd "$id" --jid "$id" --pid-file "$pid_file" --upcoming &

    ctr run --detach --env NGINX_PORT="$port" "$image" "$id"

    wait_for_file "$pid_file"

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
    pid_file="/tmp/$(unix_nano).pid"
    port=$(random_free_port)

    ctr run --pid-file "$pid_file" --env NGINX_PORT="$port" "$image" "$id" &

    wait_for_file "$pid_file" && sleep 1

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
    pid_file="/tmp/$(unix_nano).pid"
    port=$(random_free_port)

    ctr run --pid-file "$pid_file" --env NGINX_PORT="$port" --detach "$image" "$id"

    wait_for_file "$pid_file" && sleep 1

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
    port=$(random_free_port)

    cedana run containerd --jid "$jid" --env NGINX_PORT="$port" "$image"

    sleep 2

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
    pid_file="/tmp/$(unix_nano).pid"
    port=$(random_free_port)

    cedana run containerd --pid-file "$pid_file" --env NGINX_PORT="$port" --jid "$jid" --attach "$image" &

    wait_for_file "$pid_file" && sleep 2

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
    pid_file="/tmp/$(unix_nano).pid"
    port=$(random_free_port)

    ctr run --pid-file "$pid_file" --env NGINX_PORT="$port" --detach "$image" "$id"

    wait_for_file "$pid_file"

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
    pid_file="/tmp/$(unix_nano).pid"
    port=$(random_free_port)

    run cedana manage containerd "$id" --jid "$jid" --pid-file "$pid_file" --upcoming &

    ctr run --detach --env NGINX_PORT="$port" "$image" "$id"

    wait_for_file "$pid_file" && sleep 2

    run cedana dump job "$jid" --image "$new_image"
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"

    run cedana kill "$jid"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "restore container" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"
    pid_file="/tmp/$(unix_nano).pid"
    port=$(random_free_port)

    ctr run --pid-file "$pid_file" --env NGINX_PORT="$port" "$image" "$id" &

    wait_for_file "$pid_file" && sleep 1

    run cedana dump containerd "$id"
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    ctr container delete "$id"

    cedana restore containerd --path "$dump_file" --id "$id"

    run ctr task kill "$id"
}

# bats test_tags=restore
@test "restore container (detached)" {
    id=$(unix_nano)
    image="docker.io/library/nginx:latest"
    pid_file="/tmp/$(unix_nano).pid"
    port=$(random_free_port)

    ctr run --pid-file "$pid_file" --env NGINX_PORT="$port" --detach "$image" "$id"

    wait_for_file "$pid_file" && sleep 1

    run cedana dump containerd "$id"
    assert_success
    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    ctr container delete "$id"

    cedana restore containerd --path "$dump_file" --id "$id"

    run ctr task kill "$id"
}

# bats test_tags=restore
@test "restore container (new job)" {
    jid=$(unix_nano)
    image="docker.io/library/nginx:latest"
    port=$(random_free_port)

    cedana run containerd --jid "$jid" --env NGINX_PORT="$port" "$image"

    sleep 2

    run cedana dump job "$jid"
    assert_success
    dump_file=$(echo "$output" | tail -n 1  | awk '{print $NF}')
    assert_exists "$dump_file"

    debug cedana restore job "$jid" --path "$dump_file"

    run cedana job kill "$jid"
}
