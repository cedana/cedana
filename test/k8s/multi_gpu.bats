#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,gpu

load ../helpers/utils
load ../helpers/daemon # required for config env vars
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

setup_file() {
    if [ "${GPU:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU=1)"
    fi
}

#########
# Basic #
#########

# bats test_tags=deploy
@test "Deploy a GPU pod" {
    local script
    local spec

    script='gpu_smr/vector_add'
    spec=$(cmd_pod_spec_gpu "alpine:latest" "$script" 1)

    test_pod_spec DEPLOY "$spec"
}

# bats test_tags=dump
@test "Dump a GPU pod" {
    local script
    local spec

    script='gpu_smr/vector_add'
    spec=$(cmd_pod_spec_gpu "alpine:latest" "$script" 1)

    test_pod_spec DUMP "$spec"
}

# bats test_tags=restore
@test "Restore a GPU pod" {
    local script
    local spec

    script='gpu_smr/vector_add'
    spec=$(cmd_pod_spec_gpu "alpine:latest" "$script" 1)

    test_pod_spec RESTORE "$spec"
}

################
# Sample-Based #
################

# bats test_tags=dump,restore
@test "Dump/Restore: CUDA MultiGpu Vector Add" {
    local spec

    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-2xGPU-vector-add.yaml")

    test_pod_spec DUMP_RESTORE "$spec"
}

# bats test_tags=dump,restore
@test "Dump/Restore: CUDA Deepseed Training MultiGpu" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-2xGPU-deepspeed-train.yaml")

    test_pod_spec DUMP_RESTORE "$spec"
}
