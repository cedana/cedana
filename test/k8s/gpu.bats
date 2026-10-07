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

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 600 5 60
}

# bats test_tags=restore,crcr
@test "Dump/Restore/Dump/Restore a GPU pod" {
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

# bats test_tags=dump,restore,samples,multicontainer
@test "Dump/Restore: CUDA Multi-container Vector Addition" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-vector-add-multicontainer.yaml")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 600 5 60
}

#####################
# NCCL + host memory #
#####################

# Multi-process NCCL + GPU state + pinned/registered host memory; restore must resume the steps.
# Sample: cedana-samples gpu_smr/pytorch/nccl_hostmem.py

# bats test_tags=dump,restore,nccl,hostmem
@test "Dump/Restore: NCCL + host memory (2 GPUs)" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-2xGPU-torch-nccl-hostmem.yaml" "$NAMESPACE" "nccl-hostmem")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 600 5 120 "$NAMESPACE" "STEP" 300
}

# bats test_tags=restore,nccl,hostmem,declarative,scaleup
@test "Restore: NCCL + host memory scale-up onto other GPUs while the source runs" {
    [ "$(get_available_gpus)" -ge 4 ] || skip "need 4 GPUs"
    local label src_spec rep_spec src rep action_id first
    label="e2e-nccl-hostmem-scaleup-$(unix_nano)"

    labelled() { # spec -> same spec with CEDANA_CHECKPOINT=$label
        sed -i "/^      command:/i\\      env:\\n        - {name: CEDANA_CHECKPOINT, value: \"$label\"}" "$1"
        echo "$1"
    }

    src_spec=$(labelled "$(pod_spec "$SAMPLES_DIR/gpu/cuda-2xGPU-torch-nccl-hostmem.yaml" "$NAMESPACE" "nccl-hostmem-src")")
    kubectl apply -f "$src_spec"
    src=$(get_created_pod "$src_spec" "$NAMESPACE" 30)
    validate_pod "$src" 600
    wait_for_log_trigger "$src" "STEP 10" 300

    action_id=$(checkpoint_pod_by_name "$src" "$NAMESPACE")
    validate_action_id "$action_id"
    poll_action_status "$action_id" "checkpoint" 180

    rep_spec=$(labelled "$(pod_spec "$SAMPLES_DIR/gpu/cuda-2xGPU-torch-nccl-hostmem.yaml" "$NAMESPACE" "nccl-hostmem-rep")")
    kubectl apply -f "$rep_spec"
    rep=$(get_created_pod "$rep_spec" "$NAMESPACE" 30)
    validate_pod "$rep" 600
    wait_for_log_trigger "$rep" "STEP" 300

    first=$(kubectl logs "$rep" -n "$NAMESPACE" | grep -m1 -oE '^STEP [0-9]+' | awk '{print $2}')
    [ "${first:-0}" -ge 10 ] || { error_log "replica cold-started (first STEP ${first:-none})"; return 1; }
    ! kubectl logs "$rep" -n "$NAMESPACE" | grep -q '^FAIL'
    wait_for_new_log_trigger "$src" "STEP" 60 "$NAMESPACE"

    kubectl delete pod "$src" "$rep" -n "$NAMESPACE" --wait=false
}
