#!/bin/sh

# 3 GPU runs before the checkpoint (READY, then the gate file) and 3 after, each a new process

GATE=$1

run_gpu_program() {
    for _ in 1 2 3; do
        # Bounded, so a run that can't reach the controller fails rather than hangs
        timeout 60 /cedana-samples/gpu_smr/mem-throughput-saxpy || exit 1
    done
}

run_gpu_program
echo READY
while [ ! -e "$GATE" ]; do
    sleep 0.1
done
run_gpu_program
