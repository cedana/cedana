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

    # Submitting as the job user makes the job run as that user; its per-job monitor runs as root
    # (the default) or as that user (slurm.unprivileged: true). As root, both run as root.
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

##############################
# Jobs without --signal      #
##############################

# --signal=B:USR1@N only opts a job into Slurm's warnings (time limit, preemption); an on-demand
# checkpoint works without it. counting.sbatch sets it, so these jobs run counting.sh from a
# script of their own.
_submit_without_signal() {
    local script="$BATS_TEST_TMPDIR/no-signal.sh"
    printf '#!/bin/bash\nbash cpu_smr/counting.sh\n' >"$script"
    SLURM_CEDANA_SIGNAL='' slurm_submit_script "$script"
}

# Whether the job's monitor logged a line matching <pattern> on any compute node.
_monitor_logged() {
    local c
    for c in $(_slurm_compute_containers); do
        docker exec "$c" journalctl -t cedana-task-monitor --no-pager -o cat 2>/dev/null |
            grep -q -- "job=$1 $2" && return 0
    done
    return 1
}

# Waits up to <timeout> seconds for the monitor to log <pattern> for the job.
_wait_monitor_logged() {
    local job_id="$1" pattern="$2" timeout="$3" elapsed=0
    while [ "$elapsed" -lt "$timeout" ]; do
        _monitor_logged "$job_id" "$pattern" && return 0
        sleep 2
        elapsed=$((elapsed + 2))
    done
    error_log "job $job_id: no monitor line '$pattern' after ${timeout}s"
    _dump_job_failure_info "$job_id"
    return 1
}

# Whether the job's output has a line matching <pattern>, on whichever node has the file.
_job_output_has() {
    local out_file c
    out_file="$(slurm_exec scontrol show job "$1" 2>/dev/null | grep -oE 'StdOut=[^[:space:]]+' | head -1 | cut -d= -f2-)"
    [ -n "$out_file" ] || return 1
    for c in "$SLURM_CONTROLLER_CONTAINER" $(_slurm_compute_containers); do
        docker exec "$c" grep -qF -- "$2" "$out_file" 2>/dev/null && return 0
    done
    return 1
}

teardown() {
    cancel_slurm_job "${job:-}"
    cancel_slurm_job "${preemptor:-}"
}

# bats test_tags=dump,restore
@test "On demand: a job without --signal checkpoints, parks and resumes" {
    # Not local, so teardown can cancel it.
    job=$(_submit_without_signal)
    [ -n "$job" ]
    wait_for_slurm_job_state "$job" RUNNING 90
    _wait_monitor_logged "$job" "UI checkpoints on" 60

    slurm_submit_exec scancel -b -s USR1 "$job"
    _wait_monitor_logged "$job" "attempt 0: held after its checkpoint" 120

    # Release once Slurm shows the hold; earlier, the release has nothing to release.
    local i
    for i in $(seq 60); do
        slurm_exec squeue -h -j "$job" -o %r | grep -qi held && break
        sleep 2
    done
    slurm_submit_exec scontrol release "$job"
    # The release waits out Slurm's requeue delay (2 min by default).
    _wait_monitor_logged "$job" "attempt 1: restore" 480
    wait_for_slurm_job_state "$job" RUNNING 60
}

# bats test_tags=dump,restore
@test "Preemption: a job without --signal starts over and says how to opt in" {
    [ "${PREEMPT:-0}" = "1" ] || skip "preemptible partitions not configured (PREEMPT=1)"

    job=$(_submit_without_signal)
    [ -n "$job" ]
    wait_for_slurm_job_state "$job" RUNNING 90
    _wait_monitor_logged "$job" "UI checkpoints on" 60

    preemptor=$(slurm_submit_exec sbatch --parsable -p high --exclusive --wrap 'sleep 20' -o /dev/null |
        tail -1 | cut -d';' -f1 | tr -d '[:space:]')
    [ -n "$preemptor" ]

    # The partition's GraceTime, then Slurm's requeue delay (2 min by default).
    _wait_monitor_logged "$job" "attempt 1: fresh start" 480
    if _monitor_logged "$job" "attempt 0: checkpointed"; then
        error_log "job $job was checkpointed on preemption without --signal"
        return 1
    fi
    _job_output_has "$job" "no checkpoint to restore; starting from the beginning (to be checkpointed on preemption, submit with --signal=B:USR1@0)"
}
