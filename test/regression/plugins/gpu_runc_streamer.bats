#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu,runc,streamer

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/runc
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    setup_file_daemon
    do_once setup_rootfs_cuda
}

setup() {
    setup_daemon
}

teardown() {
    teardown_daemon
}

teardown_file() {
    teardown_file_daemon
}

############
### Dump ###
############

# bats test_tags=dump
@test "[$GPU_INFO] stream dump GPU container (vector add)" {
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/vector_add")"

    cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled
    watch_logs "$jid"

    sleep 1

    run cedana dump job "$jid" --streams 1
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file/img-0.gz"

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "[$GPU_INFO] stream dump GPU container (mem throughput saxpy)" {
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/mem-throughput-saxpy-loop")"

    cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled
    watch_logs "$jid"

    sleep 1

    run cedana dump job "$jid" --streams 4
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file/img-0.gz"
    assert_exists "$dump_file/img-1.gz"
    assert_exists "$dump_file/img-2.gz"
    assert_exists "$dump_file/img-3.gz"

    run cedana job kill "$jid"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "[$GPU_INFO] stream restore GPU container (vector add)" {
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/vector_add")"

    cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled
    watch_logs "$jid"

    sleep 1

    run cedana dump job "$jid" --streams 1
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file/img-0.gz"

    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
}

# bats test_tags=restore
@test "[$GPU_INFO] stream restore GPU container (mem throughput saxpy)" {
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/mem-throughput-saxpy-loop")"

    cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled
    watch_logs "$jid"

    sleep 1

    run cedana dump job "$jid" --streams 4
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file/img-0.gz"
    assert_exists "$dump_file/img-1.gz"
    assert_exists "$dump_file/img-2.gz"
    assert_exists "$dump_file/img-3.gz"

    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
}

# bats test_tags=restore,serverless
@test "[$GPU_INFO] stream restore GPU container (mem throughput saxpy, without daemon)" {
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/mem-throughput-saxpy-loop")"

    debug cedana run runc --bundle "$bundle" --gpu-enabled --no-server --detach "$jid" > /dev/null 2>&1 < /dev/null

    sleep 1

    run cedana dump runc "$jid" --streams 4
    assert_success
    dump_file=$(echo "$output" | tail -n 1 | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0.gz"
    assert_exists "$dump_file/img-1.gz"
    assert_exists "$dump_file/img-2.gz"
    assert_exists "$dump_file/img-3.gz"

    runc delete "$jid"

    debug cedana restore runc --path "$dump_file" --id "$jid" --bundle "$bundle" --detach --no-server

    wait_for_container_status "$jid" "running"
    runc kill "$jid" KILL
    wait_for_container_status "$jid" "stopped"
    runc delete "$jid"
}
