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

# The script outlives its CUDA process, and the GPU controller sends it SIGUSR2.
# The wrapper of cedana-slurm makes the script ignore the signal. See CED-2541.
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

    # The output is on the job's node.
    host=$(_get_batch_host "$job_id")
    out_file=$(grep -oP 'StdOut=\K\S+' <<<"$job_show" | head -1)
    output=$(docker exec "$host" cat "$out_file")
    [[ "$output" == *"END rc=0"* ]]
}
