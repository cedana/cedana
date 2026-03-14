#!/usr/bin/env bats

# bats file_tags=slurm,gpu

load ../helpers/utils
load ../helpers/daemon
load ../helpers/slurm
load ../helpers/slurm_propagator

##################
# GPU Workloads  #
##################

# bats test_tags=dump,restore,gpu
@test "Dump/Restore: CUDA Vector Add" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-vector-add.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 15
}

# bats test_tags=dump,restore,gpu
@test "Dump/Restore: CUDA GPU Props" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-gpu-props.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 15
}

# bats test_tags=dump,restore,gpu
@test "Dump/Restore: CUDA Compute Throughput" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-compute-throughput.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20
}

# bats test_tags=dump,restore,gpu
@test "Dump/Restore: CUDA Memory Throughput" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-mem-throughput.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20
}

# bats test_tags=dump,restore,gpu,large
@test "Dump/Restore: CUDA Memory Throughput Loop" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-mem-throughput-loop.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}

# bats test_tags=dump,restore,gpu,large
@test "Dump/Restore: CUDA Vector Add Multi-GPU" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-vector-add-multi-gpu.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}

# bats test_tags=dump,restore,gpu,large
@test "Dump/Restore: GPU PyTorch Simple" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/gpu-pytorch-simple.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 30 240
}
