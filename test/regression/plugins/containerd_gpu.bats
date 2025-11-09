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

setup_file() {
    cedana plugin install containerd/runtime-runc
    do_once pull_images
    do_once pull_latest_cedana_samples_image
    setup_file_daemon

    echo "Using CEDANA_SAMPLES_CUDA_IMAGE=$CEDANA_SAMPLES_CUDA_IMAGE"
    export CEDANA_SAMPLES_LATEST_TAG=$(get_latest_cedana_samples_tag)
    export CEDANA_SAMPLES_CUDA_IMAGE="docker.io/cedana/cedana-samples:${CEDANA_SAMPLES_LATEST_TAG}"
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

# bats test_tags=gpu, vllm
@test "[$GPU_INFO] run vLLM inference in containerd with GPU" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"
    image="$CEDANA_SAMPLES_CUDA_IMAGE"

    snapshotter="native"

    echo "=== Pre-test Checks ==="
    echo "Cedana shim runtime:"
    ls -la /usr/local/bin/cedana-shim-runc-v2 || echo "WARNING: Runtime not found!"
    
    echo "Containerd runtime config:"
    cat /etc/containerd/config.toml | grep -A 5 "runc" || echo "No runc config"
    
    echo "Containerd status:"
    ctr version || echo "WARNING: ctr not responding"

    echo "=== Checking cedana runtime ==="
    ls -la /usr/local/bin/cedana-shim-runc-v2 || echo "Runtime not found!"
    
    echo "=== Containerd config ==="
    cat /etc/containerd/config.toml | grep -A 5 "runc"

    run cedana run containerd --jid "$jid" --gpu-enabled --attach "$image" --snapshotter "$snapshotter" --  python3 /cedana-samples/gpu_smr/pytorch/llm/vllm_inference.py \
          --model 'TinyLlama/TinyLlama-1.1B-Chat-v1.0' \
          --tensor-parallel-size 1 \
          --temperature 0.1 \
          --top-p 0.9

    if [ "$status" -ne 0 ]; then
        echo "=== Containerd Logs (last 50 lines) ==="
        tail -50 /var/log/containerd.log 2>/dev/null || echo "No logs"
        
        echo "=== Failed containers ==="
        ctr containers ls || true
        ctr tasks ls || true
    fi

    assert_success
    
    run cedana ps
    assert_success
    assert_output --partial "$jid"
}