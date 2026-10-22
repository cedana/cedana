#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=base,shmem

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

# Zero-page elision and parallel dumping of shared memory and ghost files, as the Kubernetes
# chart enables them (the daemon passes them on to CRIU). CRIU's own zdtm tests cover the
# elision itself (ghost_shared_elide); this checks that cedana's options get there.
export CEDANA_CRIU_ZERO_ELIDE=true
export CEDANA_CRIU_SHMEM_DUMP_THREADS=4

HOSTMEM=/cedana-samples/cpu_smr/hostmem/hostmem.py

setup_file() {
    setup_file_daemon
}

setup() {
    setup_daemon
}

teardown() {
    stop_workload "${WORKLOAD_PID:-}"
    teardown_daemon
}

teardown_file() {
    teardown_file_daemon
}

# Processes sharing an unlinked sparse 5 GiB file (data past 4 GiB), dumped and restored
# twice: it must fit cedana's ghost limit, its holes and zero pages must be elided (the
# dump stays small), and after each restore its contents must be exact and the mapping
# still shared (see hostmem.py)
# bats test_tags=dump,restore
@test "restore processes sharing an unlinked sparse file over 4 GiB (zero pages elided)" {
    if [ ! -x "$HOSTMEM" ]; then
        skip "hostmem.py is not in this image (needs a newer cedana-samples)"
    fi

    local log gate
    log=$(mktemp /tmp/cedana-shmem-XXXXXX.log)
    gate="$log.gate"
    "$HOSTMEM" --regions ghost-sparse:$(((5 << 30) + 1000)) --dir /tmp --log "$log" --gate "$gate" &
    local pid=$!
    WORKLOAD_PID=$pid # for teardown
    wait_for_cmd 30 grep -q '^OK' "$log"

    local cycle
    for cycle in 1 2; do
        local name dumped
        name=$(unix_nano)
        cedana dump process "$pid" --name "$name" --dir /tmp --compression none

        dumped=$(du -sb "/tmp/$name" | cut -f1)
        if [ "$dumped" -gt $((64 << 20)) ]; then
            error_log "cycle $cycle: the dump is $dumped bytes for a sparse 5 GiB file: holes and zero pages not elided"
            return 1
        fi

        local lines
        lines=$(wc -l <"$log")
        cedana restore process --path "/tmp/$name"
        rm -rf "/tmp/${name:?}"
        wait_for_cmd 30 "tail -n +$((lines + 1)) $log | grep -q '^OK'" || {
            error_log "cycle $cycle: no progress after the restore"
            tail -n 5 "$log"
            return 1
        }
    done

    # A final full check, which requires every child's writes to show up again
    touch "$gate"
    wait_for_cmd 60 grep -qE '^(PASS|MISMATCH)' "$log"
    rm -f "$gate"
    run grep MISMATCH "$log"
    assert_failure
    run grep -q '^PASS' "$log"
    assert_success
}
