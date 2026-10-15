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

# The CPU job creates this file in its private /tmp and holds it open on this fd
# for its whole life, so the file has to come back with the restored job.
NAMESPACE_TMP_FILE=/tmp/namespace-marker
NAMESPACE_TMP_FD=3

# cedana's GPU controller makes an 8 GiB shared memory segment by default
# (gpu.shm_size), but the plugin gives each job a fresh /dev/shm of the
# kernel's default size, half the node's RAM, which on the CI runners is less.
# The job then fails to start with "Shared Memory Creation Error: No space
# left on device". Use a size its /dev/shm can hold. Set in cedana's config
# rather than the job's environment so the restored job gets it too.
NAMESPACE_GPU_SHM_SIZE=$((2 * 1024 * 1024 * 1024))

# The GPU controller logs under gpu.log_dir, /tmp by default, which the plugin
# makes private to the job and removes with it: a failed restore leaves no
# controller log to read. /var/tmp is neither, and the failure capture collects
# the controller's directories from there.
NAMESPACE_GPU_LOG_DIR=/var/tmp

# Set a gpu.* setting of cedana's on the compute nodes, keeping what it was.
# $1 is the key, $2 the CEDANA_GPU_* variable that sets it, $3 the value.
_set_gpu_setting() {
    local key="$1" var="$2" value="$3" c current
    for c in $(_slurm_compute_containers); do
        current="$(docker exec "$c" jq -r ".gpu.$key // empty" /etc/cedana/config.json 2>/dev/null)"
        echo "$current" >"$BATS_FILE_TMPDIR/gpu-$key.$c"

        docker exec -e "$var=$value" "$c" \
            /usr/local/bin/cedana --merge-config version >/dev/null 2>&1 || {
            error_log "Failed to set gpu.$key on $c"
            return 1
        }
        current="$(docker exec "$c" jq -r ".gpu.$key // empty" /etc/cedana/config.json 2>/dev/null)"
        if [ "$current" != "$value" ]; then
            error_log "gpu.$key on $c is '${current}', not $value"
            return 1
        fi
        info_log "gpu.$key on $c set to $value"
    done
}

# Put back the gpu.* setting _set_gpu_setting found. Same arguments, minus the value.
_restore_gpu_setting() {
    local key="$1" var="$2" c saved
    for c in $(_slurm_compute_containers); do
        [ -f "$BATS_FILE_TMPDIR/gpu-$key.$c" ] || continue
        saved="$(cat "$BATS_FILE_TMPDIR/gpu-$key.$c")"
        if [ -n "$saved" ]; then
            docker exec -e "$var=$saved" "$c" \
                /usr/local/bin/cedana --merge-config version >/dev/null 2>&1
        else
            # Rewritten in place, so the file keeps its owner and mode
            docker exec -e key="$key" "$c" sh -c '
                f=/etc/cedana/config.json
                jq "del(.gpu.$key)" "$f" >"$f.new" && cat "$f.new" >"$f" && rm -f "$f.new"
            '
        fi || error_log "Failed to restore gpu.$key on $c"
    done
}

setup_file() {
    # Runs from the cedana-samples root (see slurm_submit_script). The job's own
    # shell opens the file, so it is created inside the job's namespace and
    # owned by the submit user, and counting.sh logs a timestamp to it every
    # second.
    cat >"$BATS_FILE_TMPDIR/namespace-tmp.sbatch" <<EOF
#!/bin/bash
#SBATCH --job-name=namespace-tmp
#SBATCH --output=namespace-tmp-%j.out
#SBATCH --error=namespace-tmp-%j.err
#SBATCH --cpus-per-task=1
#SBATCH --mem=100M
#SBATCH --export=CEDANA_ENABLE=1

exec ${NAMESPACE_TMP_FD}>${NAMESPACE_TMP_FILE}
bash cpu_smr/counting.sh >&${NAMESPACE_TMP_FD}
EOF

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

    if [ "${GPU:-0}" = "1" ]; then
        _set_gpu_setting shm_size CEDANA_GPU_SHM_SIZE "$NAMESPACE_GPU_SHM_SIZE" || return 1
        _set_gpu_setting log_dir CEDANA_GPU_LOG_DIR "$NAMESPACE_GPU_LOG_DIR" || return 1
    fi
}

teardown_file() {
    _restore_gpu_setting log_dir CEDANA_GPU_LOG_DIR
    _restore_gpu_setting shm_size CEDANA_GPU_SHM_SIZE
    slurm_conf_overlay_reset
}

# Print the PID cedana monitors (and so checkpoints, or has restored) for a
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

# Find the job's node and the PID cedana monitors there, then run each given
# check as `<check> <phase> <job_id> <host> <pid>`, stopping at the first that
# fails.
_run_job_checks() {
    local phase="$1" job_id="$2"
    shift 2
    local host pid check

    host="$(_get_batch_host "$job_id")"
    [ -n "$host" ] || {
        error_log "[$phase] no batch host for job $job_id"
        return 1
    }

    pid="$(_monitored_pid "$host" "$job_id")" || {
        error_log "[$phase] no cedana-slurm monitor for job $job_id on $host"
        return 1
    }

    for check in "$@"; do
        "$check" "$phase" "$job_id" "$host" "$pid" || return 1
    done
}

# The workload must be in the private mount namespace SLURM created for the
# job. The plugin keeps each job's namespace alive by bind-mounting it at
# <BasePath>/<job_id>/.ns, so that file is SLURM's own record of it.
_check_in_job_namespace() {
    local phase="$1" job_id="$2" host="$3" pid="$4"
    local holder holder_ns workload_ns node_ns

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

# The file the job opened in its private /tmp must still be open, still owned
# by the job's user, and still hold everything written to it before the dump.
# It is read through the job's fd, which reaches it whatever namespace it is in.
_check_tmp_file_kept() {
    local phase="$1" job_id="$2" host="$3" pid="$4"
    local fd="/proc/${pid}/fd/${NAMESPACE_TMP_FD}"
    local saved="$BATS_TEST_TMPDIR/namespace-tmp"
    local owner

    docker exec "$host" cat "$fd" >"$saved.$phase" 2>/dev/null || {
        error_log "[$phase] job $job_id does not have $NAMESPACE_TMP_FILE open on fd $NAMESPACE_TMP_FD"
        return 1
    }

    owner="$(docker exec "$host" stat -L -c '%U' "$fd" 2>/dev/null)"
    if [ "$owner" != "${SLURM_SUBMIT_USER:-root}" ]; then
        error_log "[$phase] $NAMESPACE_TMP_FILE is owned by '${owner}', not ${SLURM_SUBMIT_USER:-root}"
        return 1
    fi

    info_log "[$phase] job $job_id: $NAMESPACE_TMP_FILE has $(wc -l <"$saved.$phase" | tr -d ' ') lines"

    # The job kept writing after the pre-dump check, so what was there then
    # is the start of what is there now.
    if [ "$phase" = "post-restore" ] &&
        ! head -c "$(wc -c <"$saved.pre-dump")" "$saved.$phase" | cmp -s - "$saved.pre-dump"; then
        error_log "[$phase] $NAMESPACE_TMP_FILE lost what the job wrote before the dump"
        return 1
    fi
}

# SLURM_JOB_CHECKs for test_slurm_job
_check_cpu_job() {
    _run_job_checks "$1" "$2" _check_in_job_namespace _check_tmp_file_kept
}

_check_gpu_job() {
    _run_job_checks "$1" "$2" _check_in_job_namespace
}

# The job runs from the script setup_file writes rather than a cedana-samples
# one, so it can open a file in its private /tmp.
# bats test_tags=dump,restore,samples
@test "Namespace: Dump/Restore a job in a private mount namespace (job_container/tmpfs)" {
    SLURM_JOB_SUBMIT=slurm_submit_script SLURM_JOB_CHECK=_check_cpu_job \
        test_slurm_job SUBMIT_DUMP_RESTORE "$BATS_FILE_TMPDIR/namespace-tmp.sbatch" 15
}

# GPU checkpoint/restore leans on /tmp (the controller's socket) and /dev/shm
# (the memory it shares with the job), both of which the job has to itself here.
# bats test_tags=dump,restore,samples,gpu
@test "Namespace: Dump/Restore a GPU job in a private mount namespace (job_container/tmpfs)" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-vector-add.sbatch"

    SLURM_JOB_CHECK=_check_gpu_job \
        test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}
