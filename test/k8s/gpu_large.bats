#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,gpu,large

load ../helpers/utils
load ../helpers/daemon # required for config env vars
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

setup_file() {
    if [ "${GPU:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU=1)"
    fi
    debug_log "Creating dgtest-pvc"
    spec=$(cmd_pvc_spec 50Gi dgtest-pvc)
    kubectl apply -f "$spec"
    sleep 10
    debug_log "dgtest-pvc has been applied"

    # Create HuggingFace token secret for LLM tests
    if [ -n "$HF_TOKEN" ]; then
        debug_log "Creating hf-token-secret"
        kubectl create secret generic hf-token-secret \
            --from-literal=HF_TOKEN="$HF_TOKEN" \
            -n "$NAMESPACE" \
            --dry-run=client -o yaml | kubectl apply -f -
        debug_log "hf-token-secret has been created"
    else
        debug_log "HF_TOKEN not set, skipping hf-token-secret creation"
    fi
}

#################################################
# Large Cedana Samples (LLM inference, CompBio) #
#################################################

# Blocked on CED-1864
# # bats test_tags=dump,restore,samples,llm,vllm,inference
# @test "Dump/Restore: vLLM Llama 8B Inference" {
#     local spec
#     spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-vllm-llama-3.1-8b.yaml")

#     test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300
# }

# bats test_tags=dump,restore,samples,compbio,gromacs
@test "Dump/Restore: GROMACS MD Simulation" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/gromacs-simple-example.yaml" "$NAMESPACE" "gromacs")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300
}

# # bats test_tags=dump,restore,samples,compbio,openmm
# @test "Dump/Restore: OpenMM MD Simulation" {
#     local spec
#     spec=$(pod_spec "$SAMPLES_DIR/gpu/openmm.yaml" "$NAMESPACE" "openmm")

#     test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300
# }

# Blocked on CED-1863
# # bats test_tags=dump,restore,samples,tensorflow,training
# @test "Dump/Restore: TensorFlow Training" {
#     local spec
#     spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-tensorflow-cifar100.yaml")

#     test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300 "$NAMESPACE" "epoch" 300 10
# }

# bats test_tags=dump,restore,samples,deepspeed,training
@test "Dump/Restore: DeepSpeed Training" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-deepspeed-train.yaml" "$NAMESPACE" "deepspeed")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300 "$NAMESPACE" "epoch" 300 60
}

# bats test_tags=dump,restore,samples,torch,training
@test "Dump/Restore: PyTorch Training" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-pytorch-cifar100.yaml" "$NAMESPACE" "pytorch")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300 "$NAMESPACE" "epoch" 300 10
}

# # bats test_tags=dump,restore,samples,llamafactory,training,finetuning
# @test "Dump/Restore: LlamaFactory LLM FineTuning" {
#     local spec
#     spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-llamafactory-lora-sft.yaml" "$NAMESPACE" "llamafactory")

#     test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 300 300 "$NAMESPACE" "step" 300 10
# }

# bats test_tags=dump,restore,samples,training,multi,deepspeed
@test "Dump/Restore: Multi-GPU DeepSpeed Training" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-2xGPU-deepspeed-train.yaml" "$NAMESPACE" "deepspeed-2gpu")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300 "$NAMESPACE" "epoch" 300 60
}

# bats test_tags=dump,restore,samples,training,multi,torch
@test "Dump/Restore: Multi-GPU PyTorch Training" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-4xGPU-pytorch-cifar100.yaml" "$NAMESPACE" "pytorch-4gpu")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300 "$NAMESPACE" "epoch" 300 10
}

# # bats test_tags=dump,restore,samples,training,multi,llamafactory
# @test "Dump/Restore: Multi-GPU LlamaFactory LLM FineTuning" {
#     local spec
#     spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-2xGPU-llamafactory-lora-sft.yaml" "$NAMESPACE" "llamafactory-2gpu")

#     test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300 "$NAMESPACE" "step" 300 10
# }

# Blocked on CED-1863
# # bats test_tags=dump,restore,samples,training,multi,tensorflow
# @test "Dump/Restore: Multi-GPU TensorFlow Training" {
#     local spec
#     spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-2xGPU-tensorflow-cifar100.yaml")

#     test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300 "$NAMESPACE" "step" 300 10
# }
