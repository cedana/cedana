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
@test "stream dump GPU container (vector add)" {
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/vector_add")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled
    assert_success

    sleep 1

    run cedana dump job "$jid" --streams 1
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file/img-0.gz"

    run cedana job kill "$jid"
}

# bats test_tags=dump
@test "stream dump GPU container (mem throughput saxpy)" {
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/mem-throughput-saxpy-loop")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled
    assert_success

    sleep 1

    run cedana dump job "$jid" --streams 4
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
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
@test "stream restore GPU container (vector add)" {
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/vector_add")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled
    assert_success

    sleep 1

    run cedana dump job "$jid" --streams 1
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file/img-0.gz"

    run cedana restore job "$jid"
    assert_success

    run cedana job kill "$jid"
    run cedana job delete "$jid"
}

# bats test_tags=restore
@test "stream restore GPU container (mem throughput saxpy)" {
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/mem-throughput-saxpy-loop")"

    run cedana run runc --bundle "$bundle" --jid "$jid" --gpu-enabled
    assert_success

    sleep 1

    run cedana dump job "$jid" --streams 4
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file/img-0.gz"
    assert_exists "$dump_file/img-1.gz"
    assert_exists "$dump_file/img-2.gz"
    assert_exists "$dump_file/img-3.gz"

    run cedana restore job "$jid"
    assert_success

    run cedana job kill "$jid"
    run cedana job delete "$jid"
}

# bats test_tags=restore,daemonless
@test "stream restore GPU container (mem throughput saxpy, without daemon)" {
    jid=$(unix_nano)
    bundle="$(create_samples_workload_bundle_cuda "gpu_smr/vector_add")"

    cedana run runc --bundle "$bundle" --gpu-enabled --no-server --detach "$jid" > /dev/null 2>&1 < /dev/null

    sleep 1

    run cedana dump runc "$jid" --streams 4
    assert_success

    runc delete "$jid"

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"
    assert_exists "$dump_file/img-0.gz"
    assert_exists "$dump_file/img-1.gz"
    assert_exists "$dump_file/img-2.gz"
    assert_exists "$dump_file/img-3.gz"

    run cedana restore runc --path "$dump_file" --id "$jid" --bundle "$bundle" --detach --no-server
    assert_success

    run wait_for_container_status "$jid" "running"

    runc kill "$jid" KILL
    wait_for_container_status "$jid" "stopped"
    runc delete "$jid"
}
