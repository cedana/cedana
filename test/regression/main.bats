#!/usr/bin/env bats

load helper.bash

@test "Output file created and has some data" {
    local task="./test.sh"
    local job_id="test"

    run exec_task $task $job_id

    [ -f /var/log/cedana-output.log ]
    sleep 2
    [ -s /var/log/cedana-output.log ]
}
