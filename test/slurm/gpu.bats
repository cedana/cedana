#!/usr/bin/env bats

# bats file_tags=slurm,gpu

load ../helpers/utils
load ../helpers/daemon
load ../helpers/slurm
load ../helpers/slurm_propagator

##################
# Cedana Samples #
##################

# The dump completes and uploads, but the propagator never registers a
# checkpoint for the action, so these time out. Preemption GPU C/R is
# unaffected and still runs in preemption.bats.
setup() {
    skip "GPU checkpoints are not registered by the propagator"
}

# bats test_tags=dump,restore,samples
@test "Dump/Restore: CUDA Vector Add" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-vector-add.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}

# bats test_tags=dump,restore,samples
@test "Dump/Restore: CUDA Memory Throughput (loop)" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-mem-throughput-loop.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}

# bats test_tags=dump,restore,samples,large
@test "Dump/Restore: GPU PyTorch" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/gpu-pytorch-simple.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 30 240
}
