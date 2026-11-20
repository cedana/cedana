#!/usr/bin/env bats

# CUDA graph checkpoint/restore with CEDANA_GPU_DEDUP_CAPTURE_INFO on: a dump then drops
# every capture-info query identical to the last one written for its stream. The workload
# queries its capture thousands of times per captured kernel, validates itself as the other
# cuda_graph_* workloads do, and halts on drift, so a dump that drops a query the replay
# needed fails here. Skips if the samples image predates the binary.
#
# bats file_tags=gpu,graph

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget
export CEDANA_GPU_DEDUP_CAPTURE_INFO=1    # inherited by the GPU controller from the daemon

GRAPH_CAPTURE_POLL=/cedana-samples/gpu_smr/cuda_graph_capture_poll

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    setup_file_daemon
}

# Skipped here, after the daemon is up: teardown checks on the daemon whether the test ran or
# not, and fails a test skipped from setup_file (which never started one) in persistent mode
setup() {
    setup_daemon
    [ -x "$GRAPH_CAPTURE_POLL" ] || skip "cuda_graph_capture_poll not in image (rebuild cedana-samples image)"
}

teardown() {
    teardown_daemon
}

teardown_file() {
    teardown_file_daemon
}

# The dump must have dropped queries: otherwise the flag did not reach the controller (or
# the workload stopped repeating them) and the restore below proves nothing about the dedup
assert_capture_info_dropped() {
    run grep -rhoE 'cuStreamGetCaptureInfo [0-9]+ written, [1-9][0-9]* identical dropped' "$CEDANA_GPU_LOG_DIR"
    assert_success
}

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph, polled capture) [capture-info dedup]" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- "$GRAPH_CAPTURE_POLL"
    watch_logs "$jid"

    sleep 3

    cedana dump job "$jid"
    assert_capture_info_dropped

    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 2

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
}

# Unlaunched at dump: the gate holds the first launch until after the restore
# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph, polled capture, unlaunched) [capture-info dedup]" {
    jid=$(unix_nano)
    gate=/tmp/gate-$jid
    rm -f "$gate"

    cedana run process -g --jid "$jid" -- "$GRAPH_CAPTURE_POLL" "$gate"
    watch_logs "$jid"

    sleep 3 # captured + instantiated, holding at the gate (pre-launch)

    cedana dump job "$jid"
    assert_capture_info_dropped

    cedana restore job "$jid"
    watch_logs "$jid"

    touch "$gate" # release the first launch, now restored
    sleep 2

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    rm -f "$gate"
    run cedana job kill "$jid"
}

# bats test_tags=restore,crcr
@test "[$GPU_INFO] restore->dump->restore GPU process (cuda graph, polled capture) [capture-info dedup]" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- "$GRAPH_CAPTURE_POLL"
    watch_logs "$jid"

    sleep 3

    cedana dump job "$jid"
    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 2

    cedana dump job "$jid"
    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 2

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
}
