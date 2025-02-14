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

    run cedana run containerd --image busybox --address /run/containerd/containerd.sock -n docker --jid "$jid"

    assert_success
    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"

    run cedana ps

    assert_success
    assert_output --partial "$jid"
}

@test "run container with attach" {
    jid=$(unix_nano)

    run cedana run containerd --image busybox --address /run/containerd/containerd.sock -n docker --jid "$jid" --attach

    assert_success
    assert_output --partial "hello"
}

############
### Dump ###
############

@test "dump containerd containerd without rootfs" {
    id=$(unix_nano)
    run cedana run containerd --image busybox --address /run/containerd/containerd.sock -n docker "$id"

    sleep 10

    run cedana dump containerd "$id" --image=""
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    run ctr container rm "$id"
}

