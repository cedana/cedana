#!/usr/bin/env bash

# Helper file specific for GPU tests, mostly used to instantiate workloads, weights or data

INFERENCE_MODELS=(
    "stabilityai/stablelm-2-1_6b"
)

check_huggingface_token() {
    if [ -z "$HF_TOKEN" ]; then
        echo "HF_TOKEN unset"
        exit 1
    fi
}

install_requirements() {
    TORCH_VERSION="${TORCH_VERSION:-torch2_4}"

    local req_file="/cedana-samples/requirements-${TORCH_VERSION}.txt"

    if [ ! -f "$req_file" ]; then
        echo "Requirements file not found: $req_file"
        exit 1
    fi

    echo "Installing requirements from $req_file for GPU tests"
    # runs inside container, so we can break system packages
    pip install --break-system-packages -r "$req_file" &>/dev/null
}

download_hf_models() {
    check_huggingface_token
    for model in "${INFERENCE_MODELS[@]}"; do
        echo "Downloading $model"
        python3 /cedana-samples/gpu_smr/pytorch/llm/download_hf_model.py --model $model &>/dev/null
    done
}

run_inference_test() {
    local model="$1"
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)
    sleep_duration=$((RANDOM % 11 + 10))

    run cedana run process -g --jid "$jid" -- python3 /cedana-samples/gpu_smr/pytorch/llm/transformers_inference.py --model "$model"
    assert_success

    sleep "$sleep_duration"

    run cedana dump job "$jid"
    assert_success

    dump_file=$(echo "$output" | awk '{print $NF}')
    assert_exists "$dump_file"

    sleep 5

    run cedana restore job "$jid"
    assert_success

    run cedana ps
    assert_success
    assert_output --partial "$jid"

    run cedana job kill "$jid"
}

install_requirements "$1"
download_hf_models
