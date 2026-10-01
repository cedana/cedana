#!/usr/bin/env bats

# bats file_tags=slurm,namespace

load ../helpers/utils
load ../helpers/slurm
load ../helpers/slurm_propagator

##############################
# Job Namespaces             #
##############################

# Sites isolate jobs with SLURM's namespace plugin, which gives each job a
# private /tmp and /dev/shm. Checkpoint/restore has to keep working for a job
# running inside one.
#
# job_container/tmpfs is the one config every supported version accepts: 24.11
# and 25.05 only have the job_container plugins, and 26.05 still takes
# JobContainerType, mapping it onto namespace/tmpfs.
#
# Changing this config restarts SLURM for the whole cluster, so this file runs
# in its own serial CI step rather than alongside other tests.

# Where the plugin keeps each job's namespace, on the compute node.
NAMESPACE_BASE_PATH=/var/tmp/slurm-ns

setup_file() {
    # The plugin reads job_container.conf from /etc/slurm. Without a BasePath
    # it disables itself on the node, logging that only at debug level.
    cat >"$BATS_FILE_TMPDIR/job_container.conf" <<EOF
AutoBasePath=true
BasePath=${NAMESPACE_BASE_PATH}
EOF

    slurm_conf_overlay_apply "$BATS_FILE_TMPDIR/job_container.conf" <<'EOF'
JobContainerType=job_container/tmpfs
PrologFlags=Contain
EOF

    # 26.05 reports the plugin as NamespaceType; older versions as
    # JobContainerType.
    local plugin prolog_flags
    plugin="$(slurm_conf_value NamespaceType)"
    [ -n "$plugin" ] || plugin="$(slurm_conf_value JobContainerType)"
    if [[ "$plugin" != */tmpfs ]]; then
        error_log "tmpfs namespace plugin not active (got '${plugin}')"
        return 1
    fi

    # Contain implies Alloc, so SLURM reports more flags than were set.
    prolog_flags="$(slurm_conf_value PrologFlags)"
    if [[ ",${prolog_flags}," != *,Contain,* ]]; then
        error_log "PrologFlags does not include Contain (got '${prolog_flags}')"
        return 1
    fi
}

teardown_file() {
    slurm_conf_overlay_reset
}

# Print the PID cedana monitors -- and so checkpoints or has restored -- for a
# job, from the `cedana-slurm monitor <pid> <job_id>` process on its node. The
# monitor starts a few seconds after the job does, so wait for it.
_monitored_pid() {
    local host="$1" job_id="$2" pid="" waited=0

    while [ "$waited" -lt 30 ]; do
        pid="$(docker exec "$host" ps -eo args= 2>/dev/null |
            awk -v job="$job_id" '$2 == "monitor" && $4 == job { print $3; exit }')"
        [ -n "$pid" ] && break
        sleep 1
        waited=$((waited + 1))
    done

    [ -n "$pid" ] && echo "$pid"
}

# SLURM_JOB_CHECK for test_slurm_job: the workload cedana monitors must be in
# the private mount namespace SLURM created for the job. The plugin keeps each
# job's namespace alive by bind-mounting it at <BasePath>/<job_id>/.ns, so that
# file is SLURM's own record of it.
_check_workload_in_job_namespace() {
    local phase="$1" job_id="$2"
    local host pid holder holder_ns workload_ns node_ns

    host="$(_get_batch_host "$job_id")"
    [ -n "$host" ] || {
        error_log "[$phase] no batch host for job $job_id"
        return 1
    }

    pid="$(_monitored_pid "$host" "$job_id")" || {
        error_log "[$phase] no cedana-slurm monitor for job $job_id on $host"
        return 1
    }

    holder="${NAMESPACE_BASE_PATH}/${job_id}/.ns"
    holder_ns="$(docker exec "$host" stat -L -c 'mnt:[%i]' "$holder" 2>/dev/null)" || {
        error_log "[$phase] SLURM did not create a namespace for job $job_id ($holder missing on $host)"
        return 1
    }
    workload_ns="$(docker exec "$host" readlink "/proc/${pid}/ns/mnt" 2>/dev/null)" || {
        error_log "[$phase] could not read the mount namespace of PID $pid on $host"
        return 1
    }
    node_ns="$(docker exec "$host" readlink /proc/1/ns/mnt 2>/dev/null)"

    info_log "[$phase] job $job_id: workload PID $pid in $workload_ns, SLURM's namespace $holder_ns, node $node_ns"

    if [ "$workload_ns" != "$holder_ns" ]; then
        error_log "[$phase] workload PID $pid is not in SLURM's namespace for job $job_id"
        return 1
    fi
    if [ "$workload_ns" = "$node_ns" ]; then
        error_log "[$phase] SLURM's namespace for job $job_id is the node's own"
        return 1
    fi
}

# bats test_tags=dump,restore,samples
@test "Namespace: Dump/Restore a job in a private /tmp (job_container/tmpfs)" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/cpu/counting.sbatch"

    SLURM_JOB_CHECK=_check_workload_in_job_namespace \
        test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 15
}

# GPU checkpoint/restore leans on /tmp (the controller's socket) and /dev/shm
# (the memory it shares with the job), both of which the job has to itself here.
# bats test_tags=dump,restore,samples,gpu
@test "Namespace: Dump/Restore a GPU job in a private /tmp (job_container/tmpfs)" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-vector-add.sbatch"

    SLURM_JOB_CHECK=_check_workload_in_job_namespace \
        test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}
