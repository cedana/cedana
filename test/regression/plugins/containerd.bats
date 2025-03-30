#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile

load ../helpers/utils
load ../helpers/daemon
load ../helpers/containerd

load_lib support
load_lib assert
load_lib file

###########
### Run ###
###########

# TODO SA: fix issues with log file in containerd
# TODO SA: fix issues with containerd cleanup

@test "try run container with attach without pull" {
    setup_containerd
    jid=$(unix_nano)
	ns="default"
	address="/run/containerd/containerd.sock"
	image="docker.io/library/busybox:latest"
	
    run cedana run containerd --namespace "$ns" --image "$image" "$jid" -a --address "$address"
    assert_failure

    run cedana ps

    assert_success
    refute_output --partial "$jid"
    cleanup_containerd
}

@test "run container with attach" {
    setup_containerd
    jid=$(unix_nano)
	ns="default"
	address="/run/containerd/containerd.sock"
	image="docker.io/library/busybox:latest"

    run ctr images pull "$image"
	assert_success

    run cedana run containerd --image "$image" --namespace "$ns" "$jid" -a --address "$address"
    assert_success

    run cedana ps

    assert_success
    assert_output --partial "$jid"
    cleanup_containerd
}

############
### Dump ###
############

@test "dump containerd container" {
    setup_containerd
    jid=$(unix_nano)
	ns="default"
	address="/run/containerd/containerd.sock"
	image="docker.io/library/busybox:latest"

    run ctr images pull "$image"
	assert_success

    run cedana run containerd --image "$image" --namespace "$ns" "$jid" "$address"
    assert_success

    sleep 10

    run cedana dump job "$jid"
    assert_success

	run ctr c ls | grep '$jid' | cut -d ' ' -f 1
	id="$output"

    run ctr task kill "$id"
    run ctr container rm "$id"
    cleanup_containerd
}
