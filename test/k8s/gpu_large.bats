#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,gpu,large

load ../helpers/utils
load ../helpers/daemon # required for config env vars
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

CEDANA_GPU_SHM_SIZE="${CEDANA_GPU_SHM_SIZE:-$((8*GIBIBYTE))}"

setup_file() {
    if [ "${GPU:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU=1)"
    fi
}

#################################################
# Large Cedana Samples (LLM inference, CompBio) #
#################################################

# bats test_tags=dump,restore,samples,llm,vllm,inference
@test "Dump/Restore: vLLM Llama 8B Inference" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-vllm-llama-8b.yaml")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300
}

# bats test_tags=dump,restore,samples,compbio,gromacs
@test "Dump/Restore: GROMACS MD Simulation" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/gromacs-simple-example.yaml")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300
}

# bats test_tags=dump,restore,samples,combio,openmm
@test "Dump/Restore: OpenMM MD Simulation" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/openmm.yaml")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300
}

# bats test_tags=dump,restore,samples,tensorflow,training
@test "Dump/Restore: TensorFlow Training" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-tensorflow.yaml")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300
}

# bats test_tags=dump,restore,samples,deepspeed,training
@test "Dump/Restore: DeepSpeed Training" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-deepspeed-train.yaml")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300
}
