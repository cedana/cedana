#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu,delta

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

# Delta chains restore by walking parent image dirs on disk, so images must
# stay un-tarred. CEDANA_GPU_DELTA_ENABLED is deliberately left unset: these
# tests exercise the per-request --incremental flag, which makes both the GPU
# (changed device memory chunks) and the CPU (CRIU track_mem) side incremental.
export CEDANA_CHECKPOINT_COMPRESSION=none

SAMPLES=${SAMPLES:-/cedana-samples/gpu_smr}

setup_file() {
    trap_gpu_signals
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    setup_file_daemon
}

setup() {
    trap_gpu_signals
    setup_daemon
    DUMP_DIRS=()
}

teardown() {
    if [ -n "${SAMPLE_JID:-}" ]; then
        cedana job kill "$SAMPLE_JID" &>/dev/null || true
    fi
    rm -rf "${DUMP_DIRS[@]}"
    teardown_daemon
    check_gpu_signals
}

teardown_file() {
    teardown_file_daemon
    check_gpu_signals
}

require_sample() {
    if [ ! -x "$SAMPLES/$1" ]; then
        skip "sample $1 is not in this image"
    fi
}

# Runs `cedana dump job` with the given args and sets DUMP to the directory it dumped to
dump_job() {
    run timeout 180 cedana dump job "$@"
    assert_success
    if [[ "$output" == *"unsupported on this kernel"* ]]; then
        skip "kernel lacks soft-dirty memory tracking"
    fi
    DUMP=$(grep -o 'Dumped to [^ ]*' <<<"$output" | tail -1 | cut -d' ' -f3)
    assert [ -d "$DUMP" ]
    DUMP_DIRS+=("$DUMP")
}

# Asserts that dump $2 is incremental onto dump $1, on both sides. $3 is the chain's full base
# dump (default $1), which the delta's GPU memory must be smaller than.
assert_delta_of() {
    local parent=$1 delta=$2 base=${3:-$1}
    # CPU: CRIU links the images to their parent, relatively
    run readlink "$delta/parent"
    assert_output "../$(basename "$parent")"
    # GPU: the delta manifest names the parent, and unchanged chunks aren't rewritten
    assert grep -aq "$(basename "$parent")" "$delta/gpu-delta-manifest"
    assert_gpu_smaller "$delta" "$base"
}

assert_gpu_smaller() {
    assert [ "$(stat -c %s "$1/gpu-mem-0-0")" -lt "$(stat -c %s "$2/gpu-mem-0-0")" ]
}

# XXX: after a restore, cedana-gpu compiles its dirty-hash kernel for sm_80 on any GPU (no CUDA
# context is current yet, so it falls back to "A100"), which fails to load on older GPUs: the
# dumps after a restore are then full, though still correct. Remove once that's fixed.
gpu_hashes_after_restore() {
    local cc
    cc=$(nvidia-smi --query-gpu=compute_cap --format=csv,noheader | head -1)
    [ "${cc%%.*}" -ge 8 ]
}

# Runs delta_verify gated, and waits until it's READY. It changes a few chunks of device, pageable,
# pinned (D2H), and mapped (kernel-written) memory every tick, and once the gate exists replays
# every tick from scratch: it prints PASS only if all of it came back exact.
start_delta_verify() {
    require_sample delta_verify
    jid=$(unix_nano)
    SAMPLE_JID=$jid # for teardown
    gate="/tmp/cedana-gate-$jid"
    timeout 120 cedana run process -g --jid "$jid" -- "$SAMPLES/delta_verify" 256 128 "$gate"
    wait_for_cmd 60 grep -q READY "$(logfile_for_jid "$jid")"
}

# Lets delta_verify finish after restoring the job (from its latest dump, or the given path)
restore_delta_verify() {
    touch "$gate"
    run timeout 180 cedana restore job "$jid" --attach "$@"
    rm -f "$gate"
    assert_success
    assert_output --partial PASS
    refute_output --partial MISMATCH
}

############
### Dump ###
############

# bats test_tags=dump
@test "[$GPU_INFO] dump GPU process with per-request incremental (vector add)" {
    jid=$(unix_nano)
    SAMPLE_JID=$jid

    cedana run process -g --jid "$jid" -- "$SAMPLES"/vector_add
    watch_logs "$jid"

    sleep 1

    # Base checkpoint (full), then an incremental one chained onto it
    dump_job "$jid" --incremental --leave-running
    base=$DUMP

    sleep 1

    dump_job "$jid" --incremental
    assert_output --partial "unchanged memory is in parent $base"
    assert_delta_of "$base" "$DUMP"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process from incremental chain (vector add)" {
    jid=$(unix_nano)
    SAMPLE_JID=$jid

    cedana run process -g --jid "$jid" -- "$SAMPLES"/vector_add
    watch_logs "$jid"

    sleep 1

    dump_job "$jid" --incremental --leave-running
    base=$DUMP

    sleep 1

    dump_job "$jid" --incremental
    assert_delta_of "$base" "$DUMP"

    # Restores the latest (delta) checkpoint, which walks back to the base
    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 1

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"
}

# bats test_tags=restore,verify
@test "[$GPU_INFO] restore CPU+GPU incremental chain exactly (delta_verify)" {
    start_delta_verify

    local dumps=()
    for i in 1 2 3; do
        sleep 1
        if [ "$i" -lt 3 ]; then
            dump_job "$jid" --incremental --leave-running
        else
            dump_job "$jid" --incremental
        fi
        dumps+=("$DUMP")
    done
    assert_delta_of "${dumps[0]}" "${dumps[1]}"
    assert_delta_of "${dumps[1]}" "${dumps[2]}" "${dumps[0]}"

    restore_delta_verify
}

# After a restore, the GPU side keeps chaining from the restored dump (its hashes are reloaded),
# while the CPU side starts over (a restored process is a new process, all of its pages dirty)
# bats test_tags=restore,verify
@test "[$GPU_INFO] restore mid-chain, then chain again (delta_verify)" {
    start_delta_verify

    sleep 1
    dump_job "$jid" --incremental --leave-running
    d1=$DUMP
    sleep 1
    dump_job "$jid" --incremental --leave-running
    d2=$DUMP
    sleep 1
    dump_job "$jid" --incremental
    assert_delta_of "$d2" "$DUMP" "$d1"

    # Back to the middle of the chain
    run timeout 180 cedana restore job "$jid" --path "$d2"
    assert_success

    sleep 1
    dump_job "$jid" --incremental --leave-running
    d4=$DUMP
    refute_output --partial "in parent"
    assert_not_exist "$d4/parent"
    assert grep -aq "$(basename "$d2")" "$d4/gpu-delta-manifest"
    if gpu_hashes_after_restore; then
        assert_gpu_smaller "$d4" "$d1"
    fi

    sleep 1
    dump_job "$jid" --incremental
    run readlink "$DUMP/parent"
    assert_output "../$(basename "$d4")"
    assert grep -aq "$(basename "$d4")" "$DUMP/gpu-delta-manifest"
    if gpu_hashes_after_restore; then
        assert_gpu_smaller "$DUMP" "$d1"
    fi

    restore_delta_verify
}
