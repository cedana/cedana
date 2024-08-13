#!/usr/bin/env bats

load helper.bash

setup_file() {
    BATS_NO_PARALLELIZE_WITHIN_FILE=true
}

setup() {
    # assuming WD is the root of the project
    start_cedana
    sleep 1 3>-

    # get the containing directory of this file
    # use $BATS_TEST_FILENAME instead of ${BASH_SOURCE[0]} or $0,
    # as those will point to the bats executable's location or the preprocessed file respectively
    DIR="$( cd "$( dirname "$BATS_TEST_FILENAME" )" >/dev/null 2>&1 && pwd )"
    TTY_SOCK=$DIR/tty.sock
    recvtty $TTY_SOCK &
}

teardown() {
    sleep 1 3>-

    pkill recvtty
    rm -f $TTY_SOCK

    stop_cedana
    sleep 1 3>-
}

@test "CRIO setup successful" {
    # needs to successfully connect to the crio socket
    crictl info
}

@test "Rootfs snapshot of containerd container" {
    local container_id="busybox-test"
    local image_ref="checkpoint/test:latest"
    local containerd_sock="/var/run/crio/crio.sock"
    local pod_config_path="test/regression-crio/test-data/pod-config.json"
    local container_config_path="test/regression-crio/test-data/container-config.json"
    local namespace="default"

    run start_busybox $pod_config_path $container_config_path
    run crio_rootfs_checkpoint $container_id $image_ref $containerd_sock $namespace
    echo "$output"

    [[ "$output" == *"$image_ref"* ]]
}

@test "Rootfs restore of containerd container" {
    local container_id="busybox-test-restore"
    local image_ref="checkpoint/test:latest"
    local containerd_sock="/var/run/crio/crio.sock"
    local namespace="default"

    run crio_rootfs_restore $container_id $image_ref $containerd_sock $namespace
    echo "$output"

    [[ "$output" == *"$image_ref"* ]]
}
