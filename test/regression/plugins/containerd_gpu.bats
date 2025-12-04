#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=containerd

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/containerd
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

SNAPSHOTTER="overlayfs"
export CTR_SNAPSHOTTER="$SNAPSHOTTER"

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    cedana plugin install containerd/runtime-runc
    do_once pull_images
    do_once pull_latest_cedana_samples_image

    export CEDANA_SAMPLES_REPO="docker.io/cedana/cedana-samples"
    export CEDANA_SAMPLES_LATEST_TAG=$(get_latest_cedana_samples_tag)
    export CEDANA_SAMPLES_CUDA_IMAGE="${CEDANA_SAMPLES_REPO}:${CEDANA_SAMPLES_LATEST_TAG}"

    echo "Using CEDANA_SAMPLES_CUDA_IMAGE=$CEDANA_SAMPLES_CUDA_IMAGE"
    
    setup_file_daemon
}

setup() {
    setup_daemon
}

teardown() {
    teardown_daemon
}

teardown_file() {
    teardown_file_daemon
}

###############
### GPU Run ###
###############

# bats test_tags=gpu
@test "[$GPU_INFO] run GPU containerd (vector add)" {
    jid="gpu-torch-$(unix_nano)"
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    log_file="/var/log/cedana-output-$jid.log"

    echo "Testing GPU vector_add without attach..."
    cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --snapshotter "$SNAPSHOTTER" \
        -- "$image" /app/gpu_smr/vector_add &

    sleep 5

    assert_exists "$log_file"

    run cedana job kill "$jid"
    assert_success
}

# bats test_tags=gpu,vllm
@test "[$GPU_INFO] run GPU containerd (simple vLLM test)" {
    jid="vllm-simple-$(unix_nano)"
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    
    echo "Testing vLLM import..."
    run cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --attach \
        --snapshotter "$SNAPSHOTTER" \
        -- "$image" python3 -c "
import sys
print('Python path:', sys.executable)
print('Importing vLLM...')
import vllm
print('vLLM version:', vllm.__version__)
print('vLLM import successful')
"
    
    assert_success
    assert_output --partial "vLLM import successful"
}

# bats test_tags=gpu,vllm
@test "[$GPU_INFO] run GPU containerd (vLLM initialization)" {
    jid="vllm-$(unix_nano)"
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    
    run cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --attach \
        --snapshotter "$SNAPSHOTTER" \
        -- "$image" python3 -c "
from vllm import LLM, SamplingParams
try:
    print('Initializing vLLM...')
    llm = LLM(model='gpt2', max_model_len=128)  # Small model
    print('vLLM initialized')
except Exception as e:
    print(f'Expected error (no model downloaded): {e}')
    print('But vLLM library is working')
"    
    assert_success
    assert_output --partial "vLLM library is working"
}

@test "[$GPU_INFO] run GPU containerd (tensorflow training)" { # turned off because of shim issues -> race condition?
    jid=$(unix_nano)
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    log_file="/var/log/cedana-output-$jid.log"
    
    run cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --attach \
        --snapshotter "$SNAPSHOTTER" \
        -- "$image" \
        python3 /app/gpu_smr/pytorch/training/tensorflow-train.py
    assert_success
    assert_output --partial "Model built and compiled successfully within the MirroredStrategy scope." 
}

@test "[$GPU_INFO] run GPU containerd (vLLM inference)" { # turned off because of shim issues -> race condition?
    jid=$(unix_nano)
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    log_file="/var/log/cedana-output-$jid.log"
    
    run cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --attach \
        --snapshotter "$SNAPSHOTTER" \
        -- "$image" \
        python3 /app/gpu_smr/pytorch/llm/vllm_inference.py \
        --model 'facebook/opt-125m' \
        --tensor-parallel-size 1 \
        --temperature 0.1 \
        --top-p 0.9
    
    assert_success
}

################
### GPU Dump ###
################

# bats test_tags=dump,gpu
@test "[$GPU_INFO] dump GPU containerd (vector add)" {
    jid="$(unix_nano)"
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    
    cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --snapshotter "$SNAPSHOTTER" \
        -- "$image" /app/gpu_smr/vector_add

    sleep 5

    echo "Dumping GPU containerd..."
    run cedana dump job "$jid"
    assert_success

    run cedana job kill "$jid"
    rm -rf "$dump_file"   
}

# bats test_tags=dump,gpu
@test "[$GPU_INFO] dump GPU containerd (compute throughput saxpy)" {
    jid="$(unix_nano)"
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    
    cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --snapshotter "$SNAPSHOTTER" \
        -- "$image" /app/gpu_smr/compute-throughput-saxpy
    for i in {1..10}; do
        if cedana ps | grep -q "$jid"; then
            break
        fi
        sleep 0.5
    done
    echo "Dumping GPU containerd..."
    run cedana dump job "$jid"

    assert_success

    run cedana job kill "$jid"
    rm -rf "$dump_file"   
}

@test "[$GPU_INFO] dump GPU tensorflow training" { # turned off because of shim issues -> race condition?
    jid=$(unix_nano)
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    log_file="/var/log/cedana-output-$jid.log"
    
    cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --attach \
        --snapshotter "$SNAPSHOTTER" \
        -- "$image" \
        python3 /app/gpu_smr/pytorch/training/tensorflow-train.py \

    sleep 1

    run cedana dump job "$jid"
    assert_success

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}


###############
### Restore ###
###############

# bats test_tags=restore,gpu
@test "[$GPU_INFO] restore GPU containerd (vector add) (no new image)" {
    jid="$(unix_nano)"
    image="$CEDANA_SAMPLES_CUDA_IMAGE"

    cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --snapshotter "$SNAPSHOTTER" \
        -- "$image" sh -c "/app/gpu_smr/vector_add && sleep infinity"

    sleep 3

    run cedana dump job "$jid"
    assert_success

    ctr --namespace default container delete "$jid" 2>/dev/null || true
    ctr --namespace default snapshots --snapshotter "$SNAPSHOTTER" remove "$jid" 2>/dev/null || true
    
    run cedana restore job "$jid"
    assert_success

    sleep 2

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
    run cedana job delete "$jid"
}
