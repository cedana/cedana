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

# bats test_tags=dump,restore,samples
@test "Dump/Restore: GPU PyTorch" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/gpu-pytorch-simple.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 30 240
}

# bats test_tags=dump,restore,samples
@test "Dump/Restore: CUDA Vector Add (pinned host memory)" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-vector-add-host.sbatch"

    test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}

####################
# CUDA Graph C/R   #
####################

# The cuda_graph_* workloads self-validate: every launch adds a known increment
# and a host counter shadows it, so a restore that brings back wrong device
# memory prints MISMATCH on stderr and exits non-zero.

# Gate files the warmth and siblings workloads hold their unlaunched graphs on.
# Paths match the sbatch files in cedana-samples.
GRAPH_WARMTH_GATE=/tmp/cedana-graph-warmth-gate
GRAPH_SIBLINGS_GATE=/tmp/cedana-graph-siblings-gate

_graph_job_field() {
    local job_id="$1" field="$2"

    slurm_exec scontrol show job "$job_id" |
        sed -n "s/^[[:space:]]*$field=\\(.*\\)$/\\1/p" | head -1
}

# Globs every output of the workload, not just the current job id: a restore
# gives the job a new id while the restored process keeps writing to the file it
# was checkpointed with.
_graph_grep() {
    local job_id="$1" suffix="$2" pattern="$3"
    local workdir script name c

    workdir="$(_graph_job_field "$job_id" WorkDir)"
    script="$(_graph_job_field "$job_id" Command)"
    name="$(basename "$script" .sbatch)"
    [ -n "$workdir" ] && [ -n "$name" ] || return 1

    for c in $(_slurm_compute_containers); do
        docker exec "$c" sh -c \
            "cd '$workdir' 2>/dev/null && grep -qs -- '$pattern' $name-*.$suffix" &&
            return 0
    done

    return 1
}

_graph_wait_for() {
    local job_id="$1" suffix="$2" pattern="$3" timeout="$4"
    local deadline=$((SECONDS + timeout))

    while [ "$SECONDS" -lt "$deadline" ]; do
        _graph_grep "$job_id" "$suffix" "$pattern" && return 0
        sleep 1
    done

    return 1
}

_graph_gate() {
    local action="$1" gate="$2" c

    for c in $(_slurm_compute_containers); do
        case "$action" in
        open) docker exec "$c" touch "$gate" || return 1 ;;
        reset) docker exec "$c" rm -f "$gate" || return 1 ;;
        esac
    done
}

_check_graph_mismatch() {
    local phase="$1" job_id="$2"

    if _graph_grep "$job_id" err MISMATCH; then
        error_log "[$phase] job $job_id reports MISMATCH"
        return 1
    fi
}

# SLURM_JOB_CHECKs for test_slurm_job
_check_graph_job() {
    _check_graph_mismatch "$1" "$2"
}

_check_graph_warmth_job() {
    local phase="$1" job_id="$2"

    _check_graph_mismatch "$phase" "$job_id" || return 1
    [ "$phase" = "post-restore" ] || return 0

    _graph_gate open "$GRAPH_WARMTH_GATE" || return 1

    if ! _graph_wait_for "$job_id" out "graph-warmth iter=" 60; then
        error_log "[$phase] graph never launched after the gate opened"
        return 1
    fi

    _check_graph_mismatch "$phase" "$job_id"
}

_check_graph_siblings_job() {
    local phase="$1" job_id="$2"

    _check_graph_mismatch "$phase" "$job_id" || return 1
    [ "$phase" = "post-restore" ] || return 0

    _graph_gate open "$GRAPH_SIBLINGS_GATE" || return 1

    if ! _graph_wait_for "$job_id" out "COLD LAUNCHED OK" 60; then
        error_log "[$phase] deferred graphs never launched after the gate opened"
        return 1
    fi

    _check_graph_mismatch "$phase" "$job_id"
}

# bats test_tags=dump,restore,samples
@test "Dump/Restore: CUDA Graph Loop" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-graph-loop.sbatch"

    SLURM_JOB_CHECK=_check_graph_job \
        test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}

# bats test_tags=dump,restore,samples
@test "Dump/Restore: CUDA Graph Events" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-graph-events.sbatch"

    SLURM_JOB_CHECK=_check_graph_job \
        test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}

# Checkpointed with the graph captured but never launched, so the restore has to
# bring back an instantiated graph that only runs afterwards.
# bats test_tags=dump,restore,samples
@test "Dump/Restore: CUDA Graph Warmth (cold graph)" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-graph-warmth.sbatch"

    _graph_gate reset "$GRAPH_WARMTH_GATE"

    SLURM_JOB_CHECK=_check_graph_warmth_job \
        test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}

# Warm and cold graphs of one topology live side by side across the checkpoint.
# bats test_tags=dump,restore,samples
@test "Dump/Restore: CUDA Graph Siblings (mixed warmth)" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-graph-siblings.sbatch"

    _graph_gate reset "$GRAPH_SIBLINGS_GATE"

    SLURM_JOB_CHECK=_check_graph_siblings_job \
        test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}
