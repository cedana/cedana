checkpointName=""

@test "build" {
    cd ../../
    run go build
    cd test/integration
    [[ "$status" -eq 0 ]]
}

@test "check for ctr" {
    run sudo ctr version
    [[ "$status" -eq 0 ]]
}

@test "pull busybox" {
    sudo ctr image pull quay.io/quay/busybox:latest
    sudo setsid ctr run quay.io/quay/busybox:latest testcheckpoint > /dev/null 2>&1 &
    [[ "$status" -eq 0 ]]
}

@test "checkpoint & restore" {
    output=$(sudo ../../cedana client dump -c testcheckpoint)
    
    checkpoint_line=$(echo "$output" | grep 'Checkpointing to')
    image=$(echo "$checkpoint_line" | awk '{print $NF}')

    checkpoint_line=$(echo "$output" | grep 'Checkpoint name:')
    checkpointName=$(echo "$checkpoint_line" | awk '{print $NF}')

	[[ "$status" -eq 0 ]]
    echo "$checkpointName"
    grep -B 5 Error $image/dump.log || true
    [[ "$status" -eq 0 ]]
    output=$(sudo ../../cedana client restore -i $checkpointName -c testrestore)
    [[ "$status" -eq 0 ]]
}
