#!/usr/bin/env bats

# bats file_tags=slurm,gpu

load ../helpers/utils
load ../helpers/daemon
load ../helpers/slurm
load ../helpers/slurm_propagator

##################
# Cedana Samples #
##################

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

#######################
# Controller shutdown #
#######################

# The job's bash outlives its CUDA process: an EXIT trap and a sleep after the
# sample, longer than the GPU worker's 500 ms poll. The GPU controller sends
# SIGUSR2 to the job script when its last worker ends while the script is still
# alive. The wrapper of cedana-slurm makes the job script ignore it with the
# first line of the script file it runs, so the job ends with the script's own
# exit code. Without that line the script dies of the signal and Slurm records
# FAILED 255 after the job did all its work (cedana-slurm#202,
# designs/controller-exit-race.org in cedana-slurm-example).
# bats test_tags=run,samples
@test "Run: a GPU job script that outlives its CUDA process ends COMPLETED 0" {
    local script="$BATS_TEST_TMPDIR/exit-race.sbatch"
    local job_id job_show exit_code host out_file output

    cat >"$script" <<'EOF'
#!/bin/bash
#SBATCH --job-name=exit-race
#SBATCH --output=exit-race-%j.out
#SBATCH --gres=gpu:1
trap 'echo "END rc=$?"' EXIT
gpu_smr/mem-throughput-saxpy >/dev/null
sleep 1
EOF

    job_id=$(slurm_submit_script "$script")
    [ -n "$job_id" ]

    wait_for_slurm_job_state "$job_id" "COMPLETED" 300

    job_show=$(slurm_exec scontrol show job "$job_id")
    exit_code=$(grep -oP 'ExitCode=\K\S+' <<<"$job_show" | head -1)
    [ "$exit_code" = "0:0" ]

    # The output lands in the job's working directory on its node.
    host=$(_get_batch_host "$job_id")
    out_file=$(grep -oP 'StdOut=\K\S+' <<<"$job_show" | head -1)
    output=$(docker exec "$host" cat "$out_file")
    [[ "$output" == *"END rc=0"* ]]
}
