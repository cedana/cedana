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
    spec=$(cmd_pod_spec_gpu "cedana/cedana-samples:cuda" "$script" 1)

    test_pod_spec DEPLOY "$spec" 600 5 60
}

# bats test_tags=dump
@test "Dump a GPU pod" {
    local script
    local spec

    script='gpu_smr/vector_add'
    spec=$(cmd_pod_spec_gpu "cedana/cedana-samples:cuda" "$script" 1)

    test_pod_spec DUMP "$spec" 600 5 60
}

# bats test_tags=restore
@test "Restore a GPU pod" {
    local script
    local spec

    script='gpu_smr/vector_add'
    spec=$(cmd_pod_spec_gpu "cedana/cedana-samples:cuda" "$script" 1)

    test_pod_spec RESTORE "$spec" 600 5 60
}

################
# Sample-Based #
################

# bats test_tags=dump,restore
@test "Dump/Restore: CUDA Vector Addition" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-vector-add.yaml")

    test_pod_spec DUMP_RESTORE "$spec" 600 5 60
}

# bats test_tags=dump,restore
@test "Dump/Restore: CUDA Multi-container Vector Addition" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-vector-add-multicontainer.yaml")

    test_pod_spec DUMP_RESTORE "$spec" 600 5 60
}

# bats test_tags=dump,restore
@test "Dump/Restore: CUDA Memory Throughput" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-mem-throughput.yaml")

    test_pod_spec DUMP_RESTORE "$spec" 600 5 60
}
