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

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 15 180
}

# bats test_tags=dump,restore,gpu
@test "Dump/Restore: CUDA Mem SAXPY Loop" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-mem-throughput-loop.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 15 180
}
