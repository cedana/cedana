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

# The GPU controller and workers send SIGUSR2 to the application on a premature exit. Our own
# test processes should never get it, but tests have been vanishing mid-run ("Executed N
# instead of expected M tests"). Catch it in the test (or file) process, report it, and fail
# in teardown, instead of the process dying without a result.
trap_gpu_signals() {
    GPU_SIGNAL_LOG="${BATS_TEST_TMPDIR:-$BATS_FILE_TMPDIR}/gpu-signals"
    trap gpu_signal_handler USR2
}

gpu_signal_handler() {
    local who="${BATS_TEST_NAME:-file $(basename "$BATS_TEST_FILENAME")}"
    echo "# $(date +%T) SIGUSR2 received by bats PID $BASHPID (PGID $(cut -d" " -f5 /proc/$BASHPID/stat)) in $who" |
        tee -a "$GPU_SIGNAL_LOG" >&3
}

check_gpu_signals() {
    if [ -s "$GPU_SIGNAL_LOG" ]; then
        cat "$GPU_SIGNAL_LOG"
        return 1
    fi
}
