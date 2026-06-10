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

# bats test_tags=dump,restore,preemption
@test "Preemption: Checkpoint/Restore on preempt" {
    [ "${PREEMPT:-0}" = "1" ] || skip "preemptible partitions not configured (PREEMPT=1)"

    local script="${CEDANA_SLURM_DIR}/scripts/test-preemption.sh"
    [ -f "$script" ] || skip "test-preemption.sh not found at $script"

    local compute
    compute="$(_slurm_compute_containers | awk '{print $1}')"
    [ -n "$compute" ] || skip "no compute container found"

    docker cp "$script" "${compute}:/tmp/test-preemption.sh"
    docker exec "$compute" chmod +x /tmp/test-preemption.sh

    # Preemptor demands the whole node to force eviction of the victim.
    local node_cpus
    node_cpus="$(docker exec "$compute" sinfo -h -N -o '%c' 2>/dev/null | sort -n | tail -1)"
    [[ "$node_cpus" =~ ^[0-9]+$ ]] || { error_log "could not read node CPU count from sinfo"; return 1; }

    run docker exec \
        -e LOW_PARTITION=debug \
        -e HIGH_PARTITION=high \
        -e PREEMPTOR_CPUS="$node_cpus" \
        "$compute" /tmp/test-preemption.sh
    echo "$output"
    [ "$status" -eq 0 ]
}
