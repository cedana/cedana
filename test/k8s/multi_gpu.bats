#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,gpu

load ../helpers/utils
load ../helpers/daemon # required for config env vars
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

setup_file() {
    if [ "${GPU:-0}" -lt "1" ]; then
        skip "GPU tests disabled (set GPU > 0)"
    fi
}


################
# Sample-Based #
################

# # bats test_tags=dump,restore
# @test "Dump/Restore: CUDA MultiGpu Vector Add" {
#     local spec

#     spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-2xGPU-vector-add.yaml")

#     wait_for_gpus 2
#     test_pod_spec DUMP_RESTORE "$spec"
# }

# bats test_tags=dump,restore
@test "Dump/Restore: CUDA Deepseed Training MultiGpu" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-2xGPU-deepspeed-train.yaml")

    wait_for_gpus 2
    test_pod_spec DUMP_RESTORE "$spec"
}
