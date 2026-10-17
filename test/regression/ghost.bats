#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=base,ghost

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

# Stretches CRIU's window in open_path() where processes restoring a shared ghost file with
# a busy name raced (CED-2400: ~18% of TP8 restores failed with "Can't link ...cr.N.ghost").
# The daemon passes it on to CRIU.
export CRIU_FAULT=139

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

# Processes mapping a file that was replaced under the same name, as tensor-parallel ranks
# mapping a JIT-compiled library a late rank replaced. Each restore must bring back every
# mapping, leave the new file at the name, and leave none of CRIU's ghost files behind.
# bats test_tags=dump,restore
@test "restore processes mapping a replaced (ghost) file, repeatedly" {
    local dir log
    dir=$(mktemp -d /tmp/cedana-ghost-XXXXXX)
    log="$dir/log"
    "$WORKLOADS"/ghost-busy-name.py "$dir" "$log" &
    local pid=$!
    WORKLOAD_PID=$pid # for teardown
    wait_for_cmd 30 grep -q READY "$log"

    local cycle
    for cycle in 1 2 3; do
        local name
        name=$(unix_nano)
        cedana dump process "$pid" --name "$name" --dir /tmp --compression none

        local lines
        lines=$(wc -l <"$log")
        cedana restore process --path "/tmp/$name"

        # Every process checks again after the restore, and none found a mismatch
        local process
        for process in parent child0 child1 child2 child3; do
            wait_for_cmd 30 "tail -n +$((lines + 1)) $log | grep -q '^OK $process '" || {
                error_log "cycle $cycle: $process did not run after the restore"
                tail -n 20 "$log"
                return 1
            }
        done
        run grep MISMATCH "$log"
        assert_failure

        run find "$dir" \( -name '*.cr.*' -o -name '*.cr_link*' -o -name 'link_remap*' \)
        assert_output ""
    done
}
