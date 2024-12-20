#!/usr/bin/env bats

load helper.bash

setup_file() {
    BATS_NO_PARALLELIZE_WITHIN_FILE=true
    install_cedana
}

setup() {
    # assuming WD is the root of the project
    start_cedana

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

@test "Dump + restore workload with direct remoting" {
    local task="./workload.sh"
    local job_id="workload-remoting-1"
    local bucket="direct-remoting"
    rm -rf /test

    start_cedana --bucket $bucket
    # execute, checkpoint, and restore with direct remoting
    exec_task $task $job_id
    sleep 1 3>-
    checkpoint_task $job_id /test --stream 4 --bucket $bucket
    sleep 1 3>-
    run restore_task $job_id --stream 4 --bucket $bucket
    [[ "$status" -eq 0 ]]
}
