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

    SLURM_JOB_CHECK=_check_host_memory_job \
        test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}

####################
# CUDA Graph C/R   #
####################

GRAPH_WARMTH_GATE=/tmp/cedana-graph-warmth-gate
GRAPH_SIBLINGS_GATE=/tmp/cedana-graph-siblings-gate

_graph_job_field() {
    local job_id="$1" field="$2"

    slurm_exec scontrol show job "$job_id" |
        sed -n "s/^[[:space:]]*$field=\\(.*\\)$/\\1/p" | head -1
}

_graph_grep() {
    local job_id="$1" field="$2" pattern="$3"
    local file c

    file="$(_graph_job_field "$job_id" "$field")"
    case "$file" in
    "" | *"("*) return 1 ;;
    esac

    for c in $(_slurm_compute_containers); do
        docker exec "$c" grep -qsa -- "$pattern" "$file" && return 0
    done

    return 1
}

_graph_count() {
    local job_id="$1" pattern="$2"
    local file c count

    file="$(_graph_job_field "$job_id" StdOut)"
    case "$file" in
    "" | *"("*)
        echo 0
        return 0
        ;;
    esac

    for c in $(_slurm_compute_containers); do
        count="$(docker exec "$c" sh -c "grep -ac -- '$pattern' '$file' 2>/dev/null")"
        case "$count" in
        '' | *[!0-9]*) continue ;;
        0) continue ;;
        *)
            echo "$count"
            return 0
            ;;
        esac
    done

    echo 0
}

_graph_wait_for_progress() {
    local job_id="$1" pattern="$2" timeout="$3"
    local before deadline

    before="$(_graph_count "$job_id" "$pattern")"
    deadline=$((SECONDS + timeout))

    while [ "$SECONDS" -lt "$deadline" ]; do
        [ "$(_graph_count "$job_id" "$pattern")" -gt "$before" ] && return 0
        sleep 1
    done

    return 1
}

_graph_wait_for() {
    local job_id="$1" field="$2" pattern="$3" timeout="$4"
    local deadline=$((SECONDS + timeout))

    while [ "$SECONDS" -lt "$deadline" ]; do
        _graph_grep "$job_id" "$field" "$pattern" && return 0
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

    if _graph_grep "$job_id" StdErr MISMATCH; then
        error_log "[$phase] job $job_id reports MISMATCH"
        return 1
    fi
}

# SLURM_JOB_CHECKs for test_slurm_job
_check_graph_job() {
    local phase="$1" job_id="$2"

    _check_graph_mismatch "$phase" "$job_id" || return 1
    [ "$phase" = "post-restore" ] || return 0

    if ! _graph_wait_for_progress "$job_id" "iter=" 60; then
        error_log "[$phase] no graph launches after the restore"
        return 1
    fi

    _check_graph_mismatch "$phase" "$job_id"
}

_check_host_memory_job() {
    local phase="$1" job_id="$2"

    [ "$phase" = "post-restore" ] || return 0

    if ! _graph_wait_for_progress "$job_id" "Sample result:" 60; then
        error_log "[$phase] no vector additions after the restore"
        return 1
    fi
}

_check_graph_warmth_job() {
    local phase="$1" job_id="$2"

    _check_graph_mismatch "$phase" "$job_id" || return 1
    [ "$phase" = "post-restore" ] || return 0

    _graph_gate open "$GRAPH_WARMTH_GATE" || return 1

    if ! _graph_wait_for "$job_id" StdOut "graph-warmth iter=" 60; then
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

    if ! _graph_wait_for "$job_id" StdOut "COLD LAUNCHED OK" 60; then
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

# bats test_tags=dump,restore,samples
@test "Dump/Restore: CUDA Graph Warmth (cold graph)" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-graph-warmth.sbatch"

    _graph_gate reset "$GRAPH_WARMTH_GATE"

    SLURM_JOB_CHECK=_check_graph_warmth_job \
        test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
}

# bats test_tags=dump,restore,samples
@test "Dump/Restore: CUDA Graph Siblings (mixed warmth)" {
    local sbatch_file="${SLURM_SAMPLES_DIR}/gpu/cuda-graph-siblings.sbatch"

    _graph_gate reset "$GRAPH_SIBLINGS_GATE"

    SLURM_JOB_CHECK=_check_graph_siblings_job \
        test_slurm_job SUBMIT_DUMP_RESTORE "$sbatch_file" 20 180
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
gpu_smr/mem-throughput-saxpy >/dev/null || exit $?
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
