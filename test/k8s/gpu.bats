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

# NOTE: Don't add too many tests here, as they will slow down
# the CI pipeline for every PR. Only basic sanity checks.

# bats test_tags=deploy
@test "Deploy a GPU pod" {
    local script
    local spec

    script='/cedana-samples/gpu_smr/mem-throughput-saxpy-loop'
    spec=$(cmd_pod_spec_gpu "cedana/cedana-test:cuda" "$script" 1)

    test_pod_spec DEPLOY "$spec" 600 5 60
}

# bats test_tags=dump
@test "Dump a GPU pod" {
    local script
    local spec

    script='/cedana-samples/gpu_smr/mem-throughput-saxpy-loop'
    spec=$(cmd_pod_spec_gpu "cedana/cedana-test:cuda" "$script" 1)

    test_pod_spec DEPLOY_DUMP "$spec" 600 5 60
}

# bats test_tags=restore
@test "Restore a GPU pod" {
    local script
    local spec

    script='/cedana-samples/gpu_smr/mem-throughput-saxpy-loop'
    spec=$(cmd_pod_spec_gpu "cedana/cedana-test:cuda" "$script" 1)

    test_pod_spec DEPLOY_RESTORE "$spec" 600 5 60
}

# bats test_tags=restore,crcr
@test "Dump/Restore/Dump/Restore a pod" {
    local script
    local spec

    script='/cedana-samples/gpu_smr/mem-throughput-saxpy-loop'
    spec=$(cmd_pod_spec_gpu "cedana/cedana-test:cuda" "$script" 1)

    test_pod_spec DEPLOY_DUMP_RESTORE_DUMP_RESTORE "$spec" 600 5 60
}

##################
# Cedana Samples #
##################

# bats test_tags=dump,restore,samples
@test "Dump/Restore: CUDA Vector Addition" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-vector-add.yaml")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 600 5 60
}

# bats test_tags=dump,restore,samples
@test "Dump/Restore: CUDA Multi-container Vector Addition" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-vector-add-multicontainer.yaml")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 600 5 60
}
