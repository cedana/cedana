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

@test "Check cedana --version" {
    expected_version=$(git describe --tags --always)

    actual_version=$(cedana --version)

    echo "Expected version: $expected_version"
    echo "Actual version: $actual_version"

    echo "$actual_version" | grep -q "$expected_version"

    if [ $? -ne 0 ]; then
        echo "Version mismatch: expected $expected_version but got $actual_version"
        return 1
    fi
}

@test "Daemon health check" {
    cedana daemon check
}

@test "Output file created and has some data" {
    local task="./workload.sh"
    local job_id="workload"

    # execute a process as a cedana job
    exec_task $task $job_id

    # check the output file
    sleep 1 3>-
    [ -f /var/log/cedana-output.log ]
    sleep 2 3>-
    [ -s /var/log/cedana-output.log ]
}

@test "Ensure correct logging post restore" {
    local task="./workload.sh"
    local job_id="workload2"

    # execute, checkpoint and restore a job
    exec_task $task $job_id
    sleep 2 3>-
    checkpoint_task $job_id
    sleep 2 3>-
    restore_task $job_id

    # get the post-restore log file
    local file=$(ls /var/log/ | grep cedana-output- | tail -1)
    local rawfile="/var/log/$file"

    # check the post-restore log files
    sleep 1 3>-
    [ -f $rawfile ]
    sleep 2 3>-
    [ -s $rawfile ]
}

@test "Managed job graceful exit" {
    local task="./workload.sh"
    local job_id="workload3"

    exec_task $task $job_id

    # run for a while
    sleep 2 3>-

    # kill cedana and check if the job exits gracefully
    stop_cedana
    sleep 2 3>-

    [ -z "$(pgrep -f $task)" ]
}

@test "Managed job graceful exit after restore" {
    local task="./workload.sh"
    local job_id="workload4"

    exec_task $task $job_id

    # run for a while
    sleep 2 3>-

    # checkpoint and restore the job
    checkpoint_task $job_id
    sleep 2 3>-
    restore_task $job_id

    # run for a while
    sleep 2 3>-

    # kill cedana and check if the job exits gracefully
    stop_cedana
    sleep 2 3>-

    [ -z "$(pgrep -f $task)" ]
}

@test "Custom config from CLI" {
    stop_cedana
    sleep 1 3>-

    # start cedana with custom config
    start_cedana --config='{"client":{"leave_running":true}}'
    sleep 1 3>-

    # check if the config is applied
    cedana config show
    cedana config show | grep leave_running | grep true
}

@test "Complain if GPU not enabled in daemon and using GPU flags" {
    local task="./workload.sh"
    local job_id="workload-no-gpu"

    # try to run a job with GPU flags
    run exec_task $task $job_id --gpu-enabled
    [ "$status" -ne 0 ]

    # try to dump unmanaged process with GPU flags
    run exec_task $task $job_id
    [ "$status" -eq 0 ]
    run dump_task $job_id --gpu-enabled
    [ "$status" -ne 0 ]
}

@test "Exec --attach stdout/stderr & exit code" {
    local task="./workload-2.sh"
    local job_id="workload5"

    # execute a process as a cedana job
    run exec_task $task $job_id --attach

    # check output of command
    [[ "$status" -eq 99 ]]
    [[ "$output" == *"RANDOM OUTPUT START"* ]]
    [[ "$output" == *"RANDOM OUTPUT END"* ]]
}

@test "Restore --attach stdout/stderr & exit code" {
    local task="./workload-3.sh"
    local job_id="workload6"

    # execute, checkpoint and restore a job
    exec_task $task $job_id
    sleep 1 3>-
    checkpoint_task $job_id
    sleep 1 3>-
    run restore_task $job_id --attach

    # check output of command
    [[ "$status" -eq 99 ]]
    [[ "$output" == *"RANDOM OUTPUT END"* ]]
}

@test "Rootfs snapshot of containerd container" {
    local container_id="busybox-test"
    local image_ref="checkpoint/test:latest"
    local containerd_sock="/run/containerd/containerd.sock"
    local namespace="default"

    run start_busybox $container_id
    run rootfs_checkpoint $container_id $image_ref $containerd_sock $namespace
    echo "$output"

    [[ "$output" == *"$image_ref"* ]]
}

@test "Full containerd checkpoint (jupyter notebook)" {
    local container_id="jupyter-notebook"
    local image_ref="checkpoint/test:latest"
    local containerd_sock="/run/containerd/containerd.sock"
    local namespace="default"
    local dir="/tmp/jupyter-checkpoint"

    run start_jupyter_notebook $container_id
    echo "$output"

    [[ $? -eq 0 ]] || { echo "Failed to start Jupyter Notebook"; return 1; }

    run containerd_checkpoint $container_id $image_ref $containerd_sock $namespace $dir
    echo "$output"

    [[ "$output" == *"success"* ]]
}

@test "Full containerd restore (jupyter notebook)" {
    local container_id="jupyter-notebook-restore"
    local dumpdir="/tmp/jupyter-checkpoint"

    run start_sleeping_jupyter_notebook "checkpoint/test:latest" "$container_id"

    bundle=/run/containerd/io.containerd.runtime.v2.task/default/$container_id
    pid=$(cat "$bundle"/init.pid)

    # restore the container
    run runc_restore_jupyter "$bundle" "$dumpdir" "$container_id" "$pid"
    echo "$output"

    [[ "$output" == *"Success"* ]]
}

@test "Simple runc checkpoint" {
    local rootfs="http://dl-cdn.alpinelinux.org/alpine/v3.10/releases/x86_64/alpine-minirootfs-3.10.1-x86_64.tar.gz"
    local bundle=$DIR/bundle
    echo bundle is $bundle
    local job_id="runc-test"
    local out_file=$bundle/rootfs/out
    local dumpdir=$DIR/dump

    # fetch and unpack a rootfs
    wget $rootfs

    mkdir -p $bundle/rootfs

    sudo tar -C $bundle/rootfs -xzf alpine-minirootfs-3.10.1-x86_64.tar.gz

    # create a runc container
    echo bundle is $bundle
    echo jobid is $job_id

    sudo runc run $job_id -b $bundle -d --console-socket $TTY_SOCK
    sleep 2 3>-
    sudo runc list

    # check if container running correctly, count lines in output file
    local nlines_before=$(sudo wc -l $out_file | awk '{print $1}')
    sleep 2 3>-
    local nlines_after=$(sudo wc -l $out_file | awk '{print $1}')
    [ $nlines_after -gt $nlines_before ]

    # checkpoint the container
    runc_checkpoint $dumpdir $job_id --leave-running
    [ -d $dumpdir ]

    # clean up
    sudo runc kill $job_id SIGKILL
    sudo runc delete $job_id
}

@test "Simple runc restore" {
    local bundle=$DIR/bundle
    local job_id="runc-test-restored"
    local out_file=$bundle/rootfs/out
    local dumpdir=$DIR/dump

    # restore the container
    [ -d $bundle ]
    [ -d $dumpdir ]
    echo $dumpdir contents:
    ls $dumpdir
    runc_restore $bundle $dumpdir $job_id $TTY_SOCK

    sleep 1 3>-

    # check if container running correctly, count lines in output file
    [ -f $out_file ]
    local nlines_before=$(wc -l $out_file | awk '{print $1}')
    sleep 2 3>-
    local nlines_after=$(wc -l $out_file | awk '{print $1}')
    [ $nlines_after -gt $nlines_before ]

    # clean up
    sudo runc kill $job_id SIGKILL
    sudo runc delete $job_id
}
