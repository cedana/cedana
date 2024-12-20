#!/usr/bin/env bats

load helper.bash

setup_file() {
    BATS_NO_PARALLELIZE_WITHIN_FILE=true
    install_cedana
}

setup() {
    # assuming WD is the root of the project
    start_cedana --bucket direct-remoting

    # get the containing directory of this file
    # use $BATS_TEST_FILENAME instead of ${BASH_SOURCE[0]} or $0,
    # as those will point to the bats executable's location or the preprocessed file respectively
    DIR="$( cd "$( dirname "$BATS_TEST_FILENAME" )" >/dev/null 2>&1 && pwd )"
    TTY_SOCK=$DIR/tty.sock

    cedana debug recvtty "$TTY_SOCK" &
    sleep 1 3>-
}

teardown() {
    sleep 1 3>-
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
    checkpoint_task $job_id /tmp
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

@test "Dump + restore workload with direct remoting" {
    local task="./workload.sh"
    local job_id="workload-remoting-1"
    local bucket="direct-remoting"
    rm -rf /test

    # execute, checkpoint, and restore with direct remoting
    exec_task $task $job_id
    sleep 1 3>-
    checkpoint_task $job_id /test --stream 4 --bucket $bucket
    sleep 1 3>-
    run restore_task $job_id --stream 4 --bucket $bucket
    [[ "$status" -eq 0 ]]
}
