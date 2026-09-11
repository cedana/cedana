#!/usr/bin/env bats

# bats file_tags=slurm,preemption

load ../helpers/utils
load ../helpers/slurm

##############################
# Preemption-Based C/R       #
##############################

# Runs the standalone scripts/test-preemption.sh shipped in cedana-slurm against
# the live cluster. Needs preemptible partitions (PREEMPT=1 at setup); the
# script's socket/monitor/log checks are node-local, so it runs on the compute
# node where the victim job lands.

# GPU workload the --gpu run uses, as seen inside the compute container.
SLURM_GPU_WORKLOAD="${SLURM_GPU_WORKLOAD:-/data/cedana-samples/gpu_smr/vector_add}"

# Stages the script on a compute node and sets COMPUTE + NODE_CPUS. Called
# directly, never in a subshell, so its skips take effect.
stage_preemption_script() {
    local script="${CEDANA_SLURM_DIR}/scripts/test-preemption.sh"
    [ -f "$script" ] || skip "test-preemption.sh not found at $script"

    COMPUTE="$(_slurm_compute_containers | awk '{print $1}')"
    [ -n "$COMPUTE" ] || skip "no compute container found"

    docker cp "$script" "${COMPUTE}:/tmp/test-preemption.sh"
    docker exec "$COMPUTE" chmod +x /tmp/test-preemption.sh

    # Preemptor demands the whole node to force eviction of the victim.
    NODE_CPUS="$(docker exec "$COMPUTE" sinfo -h -N -o '%c' 2>/dev/null | sort -n | tail -1)"
    [[ "$NODE_CPUS" =~ ^[0-9]+$ ]] || {
        error_log "could not read node CPU count from sinfo"
        return 1
    }

    # Submitting as the job user is what makes the monitor run unprivileged;
    # as root it would run privileged whatever the daemon mode says.
    EXEC_USER=()
    if [ -n "${SLURM_SUBMIT_USER:-}" ]; then
        EXEC_USER=(-u "$SLURM_SUBMIT_USER")
    fi

    # docker exec sets HOME but leaves the cwd at /, and the script mktemps its
    # WORKDIR under $PWD, which the exec user cannot write to. Run from that
    # user's home instead -- the same place a user would run this from.
    local exec_home
    exec_home="$(docker exec "${EXEC_USER[@]}" "$COMPUTE" sh -c 'echo "$HOME"' | tr -d '\r')"
    [ -n "$exec_home" ] || exec_home=/var/tmp
    EXEC_WORKDIR=(-w "$exec_home")

    # docker exec does not inherit this process's environment, so the script's
    # checkpoint API verification needs these forwarded explicitly.
    EXEC_ENV=(
        -e CEDANA_URL="${CEDANA_URL:-}"
        -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}"
        -e CEDANA_CLUSTER_ID="${CEDANA_CLUSTER_ID:-${SLURM_CLUSTER_ID:-}}"
    )
}

# bats test_tags=dump,restore
@test "Preemption: Checkpoint/Restore on preempt" {
    [ "${PREEMPT:-0}" = "1" ] || skip "preemptible partitions not configured (PREEMPT=1)"

    stage_preemption_script

    run docker exec \
        -e LOW_PARTITION=debug \
        -e HIGH_PARTITION=high \
        -e PREEMPTOR_CPUS="$NODE_CPUS" \
        "${EXEC_ENV[@]}" \
        "${EXEC_USER[@]}" \
        "${EXEC_WORKDIR[@]}" \
        "$COMPUTE" /tmp/test-preemption.sh
    echo "$output"
    [ "$status" -eq 0 ]
}

# bats test_tags=dump,restore,gpu
@test "Preemption: Checkpoint/Restore on preempt (GPU)" {
    [ "${PREEMPT:-0}" = "1" ] || skip "preemptible partitions not configured (PREEMPT=1)"
    [ "${GPU:-0}" = "1" ] || skip "GPU tests disabled (GPU != 1)"

    stage_preemption_script

    docker exec "$COMPUTE" test -x "$SLURM_GPU_WORKLOAD" ||
        skip "GPU workload not found at $SLURM_GPU_WORKLOAD (samples not set up?)"

    run docker exec \
        -e LOW_PARTITION=debug \
        -e HIGH_PARTITION=high \
        -e PREEMPTOR_CPUS="$NODE_CPUS" \
        "${EXEC_ENV[@]}" \
        "${EXEC_USER[@]}" \
        "${EXEC_WORKDIR[@]}" \
        "$COMPUTE" /tmp/test-preemption.sh --gpu "$SLURM_GPU_WORKLOAD"
    echo "$output"
    [ "$status" -eq 0 ]
}
