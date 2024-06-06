#!/usr/bin/env bats

load helper.bash

@test "Output file created and has some data" {
    local task="./test.sh"
    local job_id="test"

    # execute a process as a cedana job
    run exec_task $task $job_id

    # check the output file
    [ -f /var/log/cedana-output.log ]
    sleep 2
    [ -s /var/log/cedana-output.log ]
    
    # kill the process
    pid=$(ps -aux | grep $task | awk '{print $2}')
    kill -9 $pid
}

@test "Ensure correct logging post restore" {
    local task="./test.sh"
    local job_id="test2"

    # execute, checkpoint and restore a job
    run exec_task $task $job_id
    sleep 2
    run checkpoint_task $job_id
    sleep 2
    run restore_task $job_id

    # get the post-restore log file
    local file=$(ls /var/log/ | grep cedana-output- | tail -1)
    local rawfile="/var/log/$file"

    # check the post-restore log files
    [ -f $rawfile ]
    sleep 2
    [ -s $rawfile ]

    # kill the process
    pid=$(ps -aux | grep $task | awk '{print $2}')
    kill -9 $pid
}
