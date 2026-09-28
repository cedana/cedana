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

setup_file() {
    # The plugin reads job_container.conf from /etc/slurm. Without a BasePath
    # it disables itself on the node, logging that only at debug level.
    cat >"$BATS_FILE_TMPDIR/job_container.conf" <<'EOF'
AutoBasePath=true
BasePath=/var/tmp/slurm-ns
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

# bats test_tags=dump,restore,samples
@test "Namespace: Dump/Restore a job in a private /tmp (job_container/tmpfs)" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/cpu/counting.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 15
}
