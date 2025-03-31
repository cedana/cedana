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
    if [ -z "$TORCH_VERSION" ]; then
        echo "TORCH_VERSION unset"
        exit 1
    fi

    local req_file="/cedana-samples/requirements-${TORCH_VERSION}.txt"

    if [ ! -f "$req_file" ]; then
        echo "Requirements file not found: $req_file"
        exit 1
    fi

    pip install -r "$req_file"
}

download_hf_models() {
    for model in "${INFERENCE_MODELS[@]}"; do
        echo "Downloading $model"
        python gpu_smr/pytorch/llm/download_hf_model.py --model $model
    done
}

run_inference_test() {
    local model="$1"
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi

    jid=$(unix_nano)
    sleep_duration=$((RANDOM % 11 + 10))

    run cedana run process -g --jid "$jid" -- python gpu_smr/pytorch/llm/transformers_inference.py --model "$model"
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
