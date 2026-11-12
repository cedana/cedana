#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=base,cr,incremental

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

setup_file() {
    setup_file_daemon
}

setup() {
    setup_daemon
}

teardown() {
    teardown_daemon
}

teardown_file() {
    teardown_file_daemon
}

# Incremental (track_mem) CPU dumps. mem-delta.py holds a 128 MiB random buffer and rewrites
# one page per tick, and on exit verifies the whole buffer against a from-scratch rebuild.

skip_if_no_dirty_tracking() {
    if [[ "$1" == *"unsupported on this kernel"* ]]; then
        skip "kernel lacks soft-dirty memory tracking"
    fi
}

# bats test_tags=dump,restore
@test "incremental dump chains onto the previous one and restores" {
    "$WORKLOADS"/mem-delta.py 128 100 0.1 &
    pid=$!
    base=$(unix_nano)
    delta=$(unix_nano)
    sleep 2

    run cedana dump process $pid --name "$base" --dir /tmp --compression none --leave-running --incremental
    assert_success
    skip_if_no_dirty_tracking "$output"
    refute_output --partial "in parent"

    sleep 1

    run cedana dump process $pid --name "$delta" --dir /tmp --compression none --incremental
    assert_success
    assert_output --partial "unchanged memory is in parent /tmp/$base"

    # CRIU links the delta to its parent with a relative symlink
    run readlink "/tmp/$delta/parent"
    assert_output "../$base"

    # Only the few pages touched in between are in the delta
    base_kib=$(du -sk "/tmp/$base" | cut -f1)
    delta_kib=$(du -sk "/tmp/$delta" | cut -f1)
    [ "$base_kib" -gt 100000 ]
    [ "$delta_kib" -lt $((base_kib / 20)) ]

    # Restore reads unchanged pages through the chain; the workload checks every byte
    run cedana restore process --path "/tmp/$delta" --no-server
    assert_success
    assert_output --partial "OK "
    refute_output --partial "MISMATCH"
}

# bats test_tags=dump
@test "incremental dump with compression is full" {
    "$WORKLOADS"/mem-delta.py 64 100 0.1 &
    pid=$!
    base=$(unix_nano)
    next=$(unix_nano)
    sleep 2

    run cedana dump process $pid --name "$base" --dir /tmp --compression none --leave-running --incremental
    assert_success
    skip_if_no_dirty_tracking "$output"

    # A tarball can't link to a parent directory, so this is a full dump
    run cedana dump process $pid --name "$next" --dir /tmp --compression tar --leave-running --incremental
    assert_success
    refute_output --partial "in parent"
    assert_exists "/tmp/$next.tar"

    run kill $pid
}

# bats test_tags=restore
@test "restore incremental dump with missing parent fails" {
    "$WORKLOADS"/mem-delta.py 64 100 0.1 &
    pid=$!
    base=$(unix_nano)
    delta=$(unix_nano)
    sleep 2

    run cedana dump process $pid --name "$base" --dir /tmp --compression none --leave-running --incremental
    assert_success
    skip_if_no_dirty_tracking "$output"

    cedana dump process $pid --name "$delta" --dir /tmp --compression none --incremental

    rm -rf "/tmp/$base"

    run cedana restore process --path "/tmp/$delta" --no-server
    assert_failure
    assert_output --partial "parent"
}
