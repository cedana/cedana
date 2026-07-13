#!/usr/bin/env bash

# Helper file specific for GPU tests, mostly used to instantiate workloads, weights or data

INFERENCE_MODELS=(
    "stabilityai/stablelm-2-1_6b"
)

if cmd_exists nvidia-smi; then
    export GPU_MODEL=$(nvidia-smi --query-gpu=name --format=csv,noheader | head -1)
    export GPU_API=$(nvidia-smi | grep -Po '(?<=CUDA Version: )[\d.]+')
    export GPU_INFO="$GPU_MODEL, CUDA $GPU_API"
else
    export GPU_MODEL="No GPU"
    export GPU_API="N/A"
    export GPU_INFO="No GPU detected"
fi

check_huggingface_token() {
    if [ -z "$HF_TOKEN" ]; then
        error_log "HF_TOKEN unset"
        exit 1
    fi
}

install_requirements() {
    local req_file="/cedana-samples/requirements.txt"

    if [ ! -f "$req_file" ]; then
        error_log "Requirements file not found: $req_file"
        exit 1
    fi

    debug_log "Installing requirements from $req_file for GPU tests"
    # runs inside container, so we can break system packages
    pip install --break-system-packages -r "$req_file" &>/dev/null
}

download_hf_models() {
    check_huggingface_token
    for model in "${INFERENCE_MODELS[@]}"; do
        debug_log "Downloading $model"
        python3 /cedana-samples/gpu_smr/pytorch/llm/download_hf_model.py --model "$model" &>/dev/null
    done
}

# CUDA graph C/R scenario helpers (shared by gpu_cuda_graph*.bats). Return-code
# functions (no bats-assert) so a scenario is a one-line @test body.

# Highest iter=<n> the workload has logged so far (0 if none yet).
graph_max_iter() {
    local log
    log=$(logfile_for_jid "$1")
    [ -f "$log" ] || { echo 0; return; }
    grep -oE 'iter=[0-9]+' "$log" | grep -oE '[0-9]+' | sort -n | tail -1 | grep -E '.' || echo 0
}

# Poll the job log until it matches a pattern (default ~20s); non-zero on timeout.
wait_for_graph_log() {
    local jid=$1 pattern=$2 tries=${3:-100} log i
    for ((i = 0; i < tries; i++)); do
        log=$(logfile_for_jid "$jid")
        if [ -f "$log" ] && grep -qE "$pattern" "$log"; then return 0; fi
        sleep 0.2
    done
    echo "timeout waiting for '$pattern' in job $jid" >&2
    [ -f "$log" ] && cat "$log" >&2
    return 1
}

# Fail if the workload ever logged a MISMATCH (its self-check tripped).
graph_no_mismatch() {
    local log
    log=$(logfile_for_jid "$1")
    if grep -q MISMATCH "$log" 2>/dev/null; then
        echo "workload logged MISMATCH:" >&2
        grep MISMATCH "$log" >&2
        return 1
    fi
    return 0
}

# Still present in `cedana ps` and not halted.
graph_running() {
    cedana ps | grep "$1" | grep -qv halted
}

# Advancing past `before`, no mismatch, not halted.
graph_healthy() {
    local jid=$1 before=$2 after
    graph_no_mismatch "$jid" || return 1
    sleep 2 # workload prints every ~200ms
    after=$(graph_max_iter "$jid")
    if [ "$after" -le "$before" ]; then
        echo "counter did not advance: before=$before after=$after" >&2
        cat "$(logfile_for_jid "$jid")" >&2
        return 1
    fi
    graph_running "$jid" || { echo "job $jid not running / halted" >&2; return 1; }
}

# Run a self-counting graph workload, C/R, require it to keep counting. ($1 = bin)
graph_scenario_basic() {
    local bin=$1 jid
    jid=$(unix_nano)
    cedana run process -g --jid "$jid" -- "$bin" || return 1
    watch_logs "$jid"
    wait_for_graph_log "$jid" 'iter=3 ' || return 1
    local before
    before=$(graph_max_iter "$jid")
    cedana dump job "$jid" || return 1
    cedana restore job "$jid" || return 1
    watch_logs "$jid"
    graph_healthy "$jid" "$before" || { cedana job kill "$jid"; return 1; }
    cedana job kill "$jid" || true
}

# restore->dump->restore of a self-counting graph workload. ($1 = binary path)
graph_scenario_crcr() {
    local bin=$1 jid mid
    jid=$(unix_nano)
    cedana run process -g --jid "$jid" -- "$bin" || return 1
    watch_logs "$jid"
    wait_for_graph_log "$jid" 'iter=3 ' || return 1
    cedana dump job "$jid" || return 1
    cedana restore job "$jid" || return 1
    watch_logs "$jid"
    wait_for_graph_log "$jid" 'iter=5 ' || return 1
    mid=$(graph_max_iter "$jid")
    cedana dump job "$jid" || return 1
    cedana restore job "$jid" || return 1
    watch_logs "$jid"
    graph_healthy "$jid" "$mid" || { cedana job kill "$jid"; return 1; }
    cedana job kill "$jid" || true
}

# Cold: checkpoint after capture+instantiate but before any launch (gate file
# holds the first launch until after restore). ($1 = bin accepting a gate arg)
graph_scenario_cold() {
    local bin=$1 jid gate rc=0
    jid=$(unix_nano)
    gate=/tmp/gate-$jid
    rm -f "$gate"
    cedana run process -g --jid "$jid" -- "$bin" "$gate" || return 1
    watch_logs "$jid"
    wait_for_graph_log "$jid" 'READY captured unlaunched' || rc=1
    if [ "$rc" -eq 0 ] && [ "$(graph_max_iter "$jid")" -ne 0 ]; then
        echo "expected zero launches at cold checkpoint, saw $(graph_max_iter "$jid")" >&2
        rc=1
    fi
    if [ "$rc" -eq 0 ]; then
        cedana dump job "$jid" || rc=1
    fi
    if [ "$rc" -eq 0 ]; then
        cedana restore job "$jid" || rc=1
        watch_logs "$jid"
        touch "$gate" # release the first launch now that we're restored
        graph_healthy "$jid" 0 || rc=1
    fi
    rm -f "$gate"
    cedana job kill "$jid" || true
    return "$rc"
}

# Warm: checkpoint after many launches (built-up device state). ($1 = binary)
graph_scenario_warm() {
    local bin=$1 jid before
    jid=$(unix_nano)
    cedana run process -g --jid "$jid" -- "$bin" || return 1
    watch_logs "$jid"
    wait_for_graph_log "$jid" 'iter=25 ' || return 1
    before=$(graph_max_iter "$jid")
    cedana dump job "$jid" || return 1
    cedana restore job "$jid" || return 1
    watch_logs "$jid"
    graph_healthy "$jid" "$before" || { cedana job kill "$jid"; return 1; }
    cedana job kill "$jid" || true
}

# Mixed live siblings of one topology (warm + cold) alive at checkpoint; after
# restore the gate opens and the cold ones launch for the first time and must
# compute correctly. ($1 = bin accepting a gate arg)
graph_scenario_siblings() {
    local bin=$1 jid gate rc=0
    jid=$(unix_nano)
    gate=/tmp/gate-$jid
    rm -f "$gate"
    cedana run process -g --jid "$jid" -- "$bin" "$gate" || return 1
    watch_logs "$jid"
    wait_for_graph_log "$jid" 'warm_launches=10 ' || rc=1
    if [ "$rc" -eq 0 ]; then
        cedana dump job "$jid" || rc=1
    fi
    if [ "$rc" -eq 0 ]; then
        cedana restore job "$jid" || rc=1
        watch_logs "$jid"
        touch "$gate" # release first-ever launch of the deferred (cold) siblings
        wait_for_graph_log "$jid" 'COLD LAUNCHED OK' || rc=1
        graph_no_mismatch "$jid" || rc=1
        graph_running "$jid" || { echo "job $jid not running / halted" >&2; rc=1; }
    fi
    rm -f "$gate"
    cedana job kill "$jid" || true
    return "$rc"
}
