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
}

###############################################
# Large Sample-Based (LLM inference, CompBio) #
###############################################

# bats test_tags=dump,restore,llm,vllm,inference
@test "Dump/Restore: vLLM Llama 8B Inference" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cuda-vllm-llama-8b.yaml")

    test_pod_spec DUMP_RESTORE "$spec" 60 900
}

# bats test_tags=dump,restore,compbio,gromacs
@test "Dump/Restore: GROMACS MD Simulation" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gromacs-simple-example.yaml")

    test_pod_spec DUMP_RESTORE "$spec" 60 900
}

# bats test_tags=dump,restore,combio,openmm
@test "Dump/Restore: OpenMM MD Simulation" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/openmm.yaml")

    test_pod_spec DUMP_RESTORE "$spec" 60 900
}

# bats test_tags=dump,restore,tensorflow,training
@test "Dump/Restore: TensorFlow Training" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cuda-tensorflow.yaml")

    test_pod_spec DUMP_RESTORE "$spec" 60 900
}

# bats test_tags=dump,restore,deepspeed,training
@test "Dump/Restore: DeepSpeed Training" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cuda-deepspeed-train.yaml")

    test_pod_spec DUMP_RESTORE "$spec" 60 900
}
