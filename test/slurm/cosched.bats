#!/usr/bin/env bats

# bats file_tags=slurm,unprivileged,cosched

load ../helpers/utils
load ../helpers/slurm
load ../helpers/slurm_setup
load ../helpers/slurm_propagator

#############################
# Multi-user co-scheduling  #
#############################

# SLURM shares a node between users by default (ExclusiveUser=NO), so two users'
# cedana jobs routinely run side by side. Each gets its own monitor, socket and
# checkpoint; anything they share on the node (sockets under /tmp/cedana, the
# log) has to tolerate that.

# A different name each run, so a test that only works for one hardcoded user
# fails somewhere rather than everywhere.
COSCHED_NAMES=(brandon yash will neel james)
SECOND_USER="${SLURM_SECOND_USER:-${COSCHED_NAMES[$RANDOM % ${#COSCHED_NAMES[@]}]}}"
SECOND_UID="${SLURM_SECOND_UID:-2001}"
COSCHED_SAMPLE="${COSCHED_SAMPLE:-cpu/counting.sbatch}"

# Submits a long-running sample as $1. Unlike slurm_submit_job this caps memory:
# with --mem=0 a job reserves the whole node and the second user's job queues
# behind it instead of co-scheduling, which would make this test vacuous.
_cosched_submit() {
    local user="$1" out
    out=$(SLURM_SUBMIT_USER="$user" slurm_submit_exec bash -c \
        "cd /data/cedana-samples/slurm/$(dirname "$COSCHED_SAMPLE") && \
         sbatch --parsable --overcommit --export=ALL,CEDANA_ENABLE=1 \
         --cpus-per-task=1 --mem=256M '$(basename "$COSCHED_SAMPLE")'" 2>&1) || {
        error_log "sbatch as $user failed: $out"
        return 1
    }
    echo "$out" | tail -1 | cut -d';' -f1 | tr -d '[:space:]'
}

# bats test_tags=dump,samples
@test "Co-scheduling: two users' jobs checkpoint on the same node" {
    [ -n "${SLURM_SUBMIT_USER:-}" ] || skip "needs a non-root submit user (SLURM_SUBMIT_USER)"

    SLURM_SUBMIT_USER="$SECOND_USER" SLURM_SUBMIT_UID="$SECOND_UID" \
        setup_slurm_unprivileged_user

    local job_a job_b
    job_a=$(_cosched_submit "$SLURM_SUBMIT_USER")
    job_b=$(_cosched_submit "$SECOND_USER")
    [ -n "$job_a" ] && [ -n "$job_b" ]

    wait_for_slurm_job_state "$job_a" RUNNING 90
    wait_for_slurm_job_state "$job_b" RUNNING 90

    # Assert they really do share a node -- if the scheduler serialised them the
    # rest of the test would pass while proving nothing.
    local host_a host_b
    host_a=$(_get_batch_host "$job_a")
    host_b=$(_get_batch_host "$job_b")
    info_log "job $job_a on '$host_a', job $job_b on '$host_b'"
    [ -n "$host_a" ]
    [ "$host_a" = "$host_b" ]

    local action_a action_b
    action_a=$(checkpoint_slurm_job "$job_a")
    action_b=$(checkpoint_slurm_job "$job_b")

    poll_slurm_action_status "$action_a" checkpoint 180
    poll_slurm_action_status "$action_b" checkpoint 180

    cancel_slurm_job "$job_a" || true
    cancel_slurm_job "$job_b" || true
}
