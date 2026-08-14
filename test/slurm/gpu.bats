#!/usr/bin/env bats

# bats file_tags=slurm

load ../helpers/utils
load ../helpers/daemon
load ../helpers/slurm
load ../helpers/slurm_propagator

##################
# Cedana Samples #
##################

# Only workloads that run until they are checkpointed are listed here; the
# one-shot CUDA samples finish before a dump can land, and the multi-GPU one
# requests gres/gpu:2 which the test cluster cannot satisfy.

# bats test_tags=dump,restore,samples,gpu
@test "Dump/Restore: CUDA Vector Add" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-vector-add.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}

# bats test_tags=dump,restore,samples,gpu
@test "Dump/Restore: CUDA Memory Throughput (loop)" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-mem-throughput-loop.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}

# bats test_tags=dump,restore,samples,gpu,large
@test "Dump/Restore: GPU PyTorch" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/gpu-pytorch-simple.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 30 240
}
