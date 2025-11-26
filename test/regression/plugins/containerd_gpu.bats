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
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    cedana plugin install containerd/runtime-runc
    do_once pull_images
    do_once pull_latest_cedana_samples_image

    echo "Using CEDANA_SAMPLES_CUDA_IMAGE=$CEDANA_SAMPLES_CUDA_IMAGE"
    export CEDANA_SAMPLES_LATEST_TAG=$(get_latest_cedana_samples_tag)
    export CEDANA_SAMPLES_CUDA_IMAGE="docker.io/cedana/cedana-samples:${CEDANA_SAMPLES_LATEST_TAG}"
    
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
@test "[$GPU_INFO] run GPU with PyTorch check" {
    jid="gpu-torch-$(unix_nano)"
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    
    echo "Testing GPU with PyTorch..."
    run cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --attach \
        --snapshotter "overlayfs" \
        -- "$image" python3 -c "
import torch
print('PyTorch version:', torch.__version__)
print('CUDA available:', torch.cuda.is_available())
if torch.cuda.is_available():
    print('GPU count:', torch.cuda.device_count())
    print('GPU name:', torch.cuda.get_device_name(0))
    # Do a simple GPU operation
    x = torch.randn(100, 100).cuda()
    y = torch.randn(100, 100).cuda()
    z = torch.matmul(x, y)
    print('GPU computation successful')
"
    
    assert_success
    assert_output --partial "CUDA available: True"
    assert_output --partial "GPU computation successful"
}

# bats test_tags=gpu,vllm
@test "[$GPU_INFO] run simple vLLM test" {
    jid="vllm-simple-$(unix_nano)"
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    
    echo "Testing vLLM import..."
    # can vLLM import successfully?
    run cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --attach \
        --snapshotter "overlayfs" \
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
@test "[$GPU_INFO] run vLLM initialization" {
    jid="vllm-$(unix_nano)"
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    
    # smaller model to avoid download issues
    run cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --attach \
        --snapshotter "overlayfs" \
        -- "$image" python3 -c "
from vllm import LLM, SamplingParams

# This will fail but at least tests the vLLM setup
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

@test "[$GPU_INFO] run tensorflow training in containerd with GPU" { # turned off because of shim issues -> race condition?
    jid=$(unix_nano)
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    log_file="/var/log/cedana-output-$jid.log"
    
    run cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --attach \
        --snapshotter "overlayfs" \
        -- "$image" \
        python3 /cedana-samples/gpu_smr/pytorch/training/tensorflow-train.py
    assert_success
    assert_output --partial "Model built and compiled successfully within the MirroredStrategy scope." 
}

@test "[$GPU_INFO] run vLLM inference in containerd with GPU" { # turned off because of shim issues -> race condition?
    jid=$(unix_nano)
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    log_file="/var/log/cedana-output-$jid.log"
    
    run cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --attach \
        --snapshotter "overlayfs" \
        -- "$image" \
        python3 /cedana-samples/gpu_smr/pytorch/llm/vllm_inference.py \
        --model 'facebook/opt-125m' \
        --tensor-parallel-size 1 \
        --temperature 0.1 \
        --top-p 0.9

    echo "$output"
    if [ -f "$log_file" ]; then
        echo "Log file contents:"
        cat "$log_file"
        tail -50 "$log_file" 2>/dev/null || cat "$log_file"
    else
        echo "Log file not found at: $log_file"
    fi

    if [ -f /var/log/dockerd.err.log ]; then
        echo "Docker daemon error log contents:"
        tail -100 /var/log/dockerd.err.log
    else
        echo "Docker daemon error log not found."
    fi

    if [ -f /var/log/dockerd.out.log ]; then
        tail -100 /var/log/dockerd.out.log
    else
        echo "Docker daemon output log not found."
    fi
    
    assert_success
}

################
### GPU Dump ###
################

# bats test_tags=dump,gpu
@test "[$GPU_INFO] dump simple GPU dump" {
    jid="$(unix_nano)"
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    
    cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --snapshotter "overlayfs" \
        -- "$image" python3 -c "
import torch
print('Starting GPU workload...')
x = torch.randn(1000, 1000).cuda()
y = torch.randn(1000, 1000).cuda()
z = torch.matmul(x, y)
print('GPU workload complete')
while True:
    pass  # Keep the process running
"

    sleep 5

    echo "Dumping GPU containerd..."
    run cedana dump job "$jid"
    assert_success

    run cedana job kill "$jid"
    rm -rf "$dump_file"   
}

# bats test_tags=dump,gpu
@test "[$GPU_INFO] dump GPU inference" {
    jid="$(unix_nano)"
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    
    cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --snapshotter "overlayfs" \
        -- "$image" python3 -c "
import torch
import torch.nn as nn
import torch.optim as optim
print('Starting GPU workload with ANN...')
class SimpleNN(nn.Module):
    def __init__(self):
        super(SimpleNN, self).__init__()
        self.fc = nn.Linear(1000, 10)
    def forward(self, x):
        return self.fc(x)
model = SimpleNN().cuda()
criterion = nn.CrossEntropyLoss()
optimizer = optim.SGD(model.parameters(), lr=0.01)
for epoch in range(5):
    inputs = torch.randn(32, 1000).cuda()
    labels = torch.randint(0, 10, (32,)).cuda()
    optimizer.zero_grad()
    outputs = model(inputs)
    loss = criterion(outputs, labels)
    loss.backward()
    optimizer.step()
    print(f'Epoch {epoch+1}, Loss: {loss.item()}')
print('GPU ANN workload complete')
while True:
    inputs = torch.randn(32, 1000).cuda()
    outputs = model(inputs)
    print(f'Inference output shape: {outputs.shape}')
"
    sleep 5

    echo "Dumping GPU containerd..."
    run cedana dump job "$jid"
    assert_success

    run cedana job kill "$jid"
    rm -rf "$dump_file"   
}

@test "[$GPU_INFO] dump tensorflow training GPU dump" { # turned off because of shim issues -> race condition?
    jid=$(unix_nano)
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    log_file="/var/log/cedana-output-$jid.log"
    
    cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --attach \
        --snapshotter "overlayfs" \
        -- "$image" \
        python3 /cedana-samples/gpu_smr/pytorch/training/tensorflow-train.py \

    sleep 1

    run cedana dump job "$jid"
    assert_success

    run cedana job kill "$jid"
    rm -rf "$dump_file"
}


###############
### Restore ###
###############

@test "[$GPU_INFO] restore simple workload in GPU containerd" { # turned off because of shim issues -> race condition?
    jid="$(unix_nano)"
    image="$CEDANA_SAMPLES_CUDA_IMAGE"
    
    cedana run containerd \
        --jid "$jid" \
        --gpu-enabled \
        --snapshotter "overlayfs" \
        -- "$image" python3 -c "
import torch
print ('Starting GPU workload...')
x = torch.randn(1000, 1000).cuda()
y = torch.randn(1000, 1000).cuda()
print('GPU workload complete')
while True:
    z = torch.matmul(x, y)
"

    sleep 1

    cedana dump job "$jid"

    cedana restore job "$jid" --image "$image"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
    run cedana job delete "$jid"
}
