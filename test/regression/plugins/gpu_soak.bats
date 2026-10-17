#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# GPU soak: the same GPU checkpoint/restore many times, to catch failures that only happen
# 1 in N times. Runs nightly (the soak tag), counting every failure.
#
# bats file_tags=gpu,soak

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget

SOAK_ITERATIONS=${SOAK_ITERATIONS:-50}

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
}

teardown() {
    teardown_daemon
    check_gpu_signals
}

teardown_file() {
    teardown_file_daemon
    check_gpu_signals
}

# Results vector_add printed since line $2 of its log, that aren't A[i] + B[i] = 2.5 i (to the
# 6 significant digits it prints)
wrong_results() {
    tail -n +"$2" "$1" | awk -F'[][= ]+' '/^Sample result/ {
        i = $4; v = $5 + 0; want = i * 2.5
        d = v - want; if (d < 0) d = -d
        if (d > want * 1e-5 + 1e-3) print "h_C[" i "] = " $5 ", want " want
    }'
}

# One GPU job dumped and restored again and again: each restore keeps computing right
# bats test_tags=dump,restore
@test "[$GPU_INFO] soak: dump and restore a GPU process, $SOAK_ITERATIONS times in a row" {
    local jid log
    jid=$(unix_nano)
    cedana run process -g --jid "$jid" -- /cedana-samples/gpu_smr/vector_add
    log=$(logfile_for_jid "$jid")
    wait_for_cmd 60 grep -q 'Sample result' "$log"

    local i failures=0 first_failure="" first_error=""
    for i in $(seq 1 "$SOAK_ITERATIONS"); do
        local lines output
        lines=$(wc -l <"$log")
        if ! output=$(cedana dump job "$jid" 2>&1) || ! output=$(cedana restore job "$jid" 2>&1); then
            failures=$((failures + 1))
            first_failure=${first_failure:-$i}
            first_error=${first_error:-$(tail -n 3 <<<"$output")}
            # Nothing to continue from if the job is gone
            cedana ps 2>/dev/null | grep "$jid" | grep -qv halted || break
            continue
        fi
        # Running again, computing, and computing right
        if ! wait_for_cmd 30 "tail -n +$((lines + 1)) $log | grep -q 'Sample result'"; then
            failures=$((failures + 1))
            first_failure=${first_failure:-$i}
            first_error=${first_error:-"no results after the restore"}
        elif [ -n "$(wrong_results "$log" $((lines + 1)))" ]; then
            failures=$((failures + 1))
            first_failure=${first_failure:-$i}
            first_error=${first_error:-"wrong results after the restore: $(wrong_results "$log" $((lines + 1)) | head -n 1)"}
        fi
    done

    cedana job kill "$jid" || true
    if [ "$failures" -gt 0 ]; then
        error_log "$failures of $SOAK_ITERATIONS GPU dump/restore cycles failed (first: cycle $first_failure: $first_error)"
        return 1
    fi
}
