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

# bats test_tags=dump,restore,samples
@test "Dump/Restore: NumPy Matrix Ops" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/cpu/numpy-matrix-ops.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20
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

# bats test_tags=dump,restore,embedded
# Requires cedana-slurm to have CAP_SYS_PTRACE,CAP_DAC_READ_SEARCH,CAP_CHECKPOINT_RESTORE:
#   setcap cap_dac_read_search,cap_sys_ptrace,cap_checkpoint_restore=eip /usr/local/bin/cedana-slurm
@test "Dump/Restore: Embedded (Timestamp Logger)" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/cpu/counting.sbatch"

    restart_cedana_slurm_daemon_unprivileged
    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 15
    restart_cedana_slurm_daemon
}
