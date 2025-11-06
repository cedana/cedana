#!/usr/bin/env bash

# Helper file specific for GPU tests, mostly used to instantiate workloads, weights or data

INFERENCE_MODELS=(
    "stabilityai/stablelm-2-1_6b"
)

if cmd_exists nvidia-smi; then
    export GPU_MODEL=$(nvidia-smi --query-gpu=name --format=csv,noheader | head -1)
    export GPU_API=$(nvidia-smi | grep -Po '(?<=CUDA Version: )[\d.]+')
    export GPU_INFO="$GPU_MODEL, CUDA $GPU_API"
else
    export GPU_MODEL="No GPU"
    export GPU_API="N/A"
    export GPU_INFO="No GPU detected"
fi

check_huggingface_token() {
    if [ -z "$HF_TOKEN" ]; then
        error_log "HF_TOKEN unset"
        exit 1
    fi
}

install_requirements() {
    local req_file="/cedana-samples/requirements.txt"

    if [ ! -f "$req_file" ]; then
        error_log "Requirements file not found: $req_file"
        exit 1
    fi

    debug_log "Installing requirements from $req_file for GPU tests"
    # runs inside container, so we can break system packages
    pip install --break-system-packages -r "$req_file" &>/dev/null
}

download_hf_models() {
    check_huggingface_token
    for model in "${INFERENCE_MODELS[@]}"; do
        debug_log "Downloading $model"
        python3 /cedana-samples/gpu_smr/pytorch/llm/download_hf_model.py --model "$model" &>/dev/null
    done
}
