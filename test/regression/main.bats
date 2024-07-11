#!/usr/bin/env bats

load helper.bash

@test "Output file created and has some data" {
    local task="./test.sh"
    local job_id="test"

    # execute a process as a cedana job
    run exec_task $task $job_id

    # check the output file
    [ -f /var/log/cedana-output.log ]
    sleep 2 3>- &
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
    sleep 2 3>- &
    run checkpoint_task $job_id
    sleep 2 3>- &
    run restore_task $job_id

    # get the post-restore log file
    local file=$(ls /var/log/ | grep cedana-output- | tail -1)
    local rawfile="/var/log/$file"

    # check the post-restore log files
    [ -f $rawfile ]
    sleep 2 3>- &
    [ -s $rawfile ]

    # kill the process
    pid=$(ps -aux | grep $task | awk '{print $2}')
    kill -9 $pid
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

@test "Rootfs restore of containerd container" {
  local container_id="busybox-test-restore"
  local image_ref="checkpoint/test:latest"
  local containerd_sock="/run/containerd/containerd.sock"
  local namespace="default"


  run rootfs_restore $container_id $image_ref $containerd_sock $namespace
  echo "$output"

  [[ "$output" == *"$image_ref"* ]]
}

@test "Simple runc checkpoint" {
  local rootfs="http://dl-cdn.alpinelinux.org/alpine/v3.10/releases/x86_64/alpine-minirootfs-3.10.1-x86_64.tar.gz"
  local bundle=$(pwd)/bundle
  echo bundle is $bundle
  local job_id="runc-test"
  local out_file=$bundle/rootfs/out
  local dumpdir=$(pwd)/dump
  local tty_sock=$(pwd)/tty.sock

  # fetch and unpack a rootfs
  wget $rootfs
  mkdir -p $bundle/rootfs
  sudo chown root:root $bundle
  sudo tar -C $bundle/rootfs -xzf alpine-minirootfs-3.10.1-x86_64.tar.gz
  # cp config.json $bundle/config.json

  # create a runc container
  recvtty $tty_sock &
  local tty_pid=$!
  echo bundle is $bundle
  echo jobid is $job_id
  sudo runc run $job_id -b $bundle -d --console-socket $tty_sock
  sudo runc list
  sleep 1 3>- &

  # check if container running correctly, count lines in output file
  run sudo test -f "$out_file"
  [ "$status" -eq 0 ]
  local nlines_before=$(sudo wc -l $out_file | awk '{print $1}')
  sleep 2 3>- &
  local nlines_after=$(sudo wc -l $out_file | awk '{print $1}')
  [ $nlines_after -gt $nlines_before ]

  # checkpoint the container
  run runc_checkpoint $dumpdir $job_id
  [ -d $dumpdir ]

  # clean up
  sudo runc kill $job_id SIGKILL
  sudo runc delete $job_id
  # kill -9 $tty_pid
  sudo rm -rf $tty_sock
}

@test "Simple runc restore" {
  local bundle=$(pwd)/bundle
  local job_id="runc-test-restored"
  local out_file=$bundle/rootfs/out
  local dumpdir=$(pwd)/dump
  local tty_sock=$(pwd)/tty.sock

  # restore the container
  [ -d $bundle ]
  [ -d $dumpdir ]
  recvtty $tty_sock &
  local tty_pid=$!
  run runc_restore $bundle $dumpdir $job_id $tty_sock

  sleep 1 3>- &

  # check if container running correctly, count lines in output file
  [ -f $out_file ]
  local nlines_before=$(wc -l $out_file | awk '{print $1}')
  sleep 2 3>- &
  local nlines_after=$(wc -l $out_file | awk '{print $1}')
  [ $nlines_after -gt $nlines_before ]

  # clean up
  sudo runc kill $job_id SIGKILL
  sudo runc delete $job_id
  # kill -9 $tty_pid
  sudo rm -rf $tty_sock
}

