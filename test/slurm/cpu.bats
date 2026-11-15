#!/usr/bin/env bats

# bats file_tags=slurm

load ../helpers/utils
load ../helpers/daemon
load ../helpers/slurm
load ../helpers/slurm_propagator

##################
# Cedana Samples #
##################

# bats test_tags=dump,restore,samples
@test "Dump/Restore: Timestamp Logger (counting)" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/cpu/counting.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 15
}

# bats test_tags=dump,restore,samples
@test "Dump/Restore: Monte Carlo Pi" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/cpu/monte-carlo-pi.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 15
}

# bats test_tags=dump,restore,samples
@test "Dump/Restore: Password Hashing Benchmark" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/cpu/password-hashing-benchmark.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 15
}

# A Go 1.25+ program keeps its cgroup's CPU-limit file open; the restore runs under a new cgroup.
# bats test_tags=dump,restore,samples
@test "Dump/Restore: Go 1.25 counting" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/cpu/go-counting.sbatch"
    slurm_submit_exec test -f "$(_slurm_sample_container_dir "$sbatch_file")/$(basename "$sbatch_file")" ||
        skip "this cedana-samples has no go-counting sample"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 15
}

# bats test_tags=dump,restore,samples,large
@test "Dump/Restore: CPU PyTorch" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/cpu/cpu-pytorch.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 30 180
}

# bats test_tags=dump,restore,samples,large
@test "Dump/Restore: Scikit-Learn Random Forest" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/cpu/sklearn-random-forest.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}

# bats test_tags=dump,restore,samples,large
@test "Dump/Restore: XGBoost Training" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/cpu/xgboost-training.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}
