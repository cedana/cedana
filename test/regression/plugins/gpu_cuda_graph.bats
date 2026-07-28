#!/usr/bin/env bats

# CUDA graph checkpoint/restore. The cuda_graph_* workloads self-validate (a host
# counter shadows a graph-incremented device buffer) and exit non-zero on any
# drift, so these tests just run each scenario through C/R and check the job is
# still alive afterwards -- a corrupted restore trips the workload's own check and
# the job halts.
#
# Requires the cuda_graph_* binaries from the cedana-samples image; skips if the
# image predates them (same convention as cuda_samples.bats).
#
# bats file_tags=gpu

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget

GRAPH_LOOP=/cedana-samples/gpu_smr/cuda_graph_loop
GRAPH_EVENTS=/cedana-samples/gpu_smr/cuda_graph_events
GRAPH_WARMTH=/cedana-samples/gpu_smr/cuda_graph_warmth
GRAPH_SIBLINGS=/cedana-samples/gpu_smr/cuda_graph_siblings

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    for bin in "$GRAPH_LOOP" "$GRAPH_EVENTS" "$GRAPH_WARMTH" "$GRAPH_SIBLINGS"; do
        [ -x "$bin" ] || skip "cuda_graph samples not in image (rebuild cedana-samples image)"
    done
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

###########
### Run ###
###########

@test "[$GPU_INFO] run GPU process (cuda graph capture loop)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- "$GRAPH_LOOP"
    watch_logs "$jid"

    sleep 2

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph capture loop)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- "$GRAPH_LOOP"
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

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph events)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- "$GRAPH_EVENTS"
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

# bats test_tags=restore,crcr
@test "[$GPU_INFO] restore->dump->restore GPU process (cuda graph events)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- "$GRAPH_EVENTS"
    watch_logs "$jid"

    sleep 2

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

# Warm: checkpoint after the graph has been launched many times.
# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph, warm checkpoint)" {
    jid=$(unix_nano)

    cedana run process -g --jid "$jid" -- "$GRAPH_WARMTH"
    watch_logs "$jid"

    sleep 3

    cedana dump job "$jid"

    cedana restore job "$jid"
    watch_logs "$jid"

    sleep 2

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    run cedana job kill "$jid"
}

# Cold: checkpoint after capture+instantiate but before any launch. The gate file
# holds the first launch until after restore, so the dump lands unlaunched.
# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph, cold checkpoint / unlaunched)" {
    jid=$(unix_nano)
    gate=/tmp/gate-$jid
    rm -f "$gate"

    cedana run process -g --jid "$jid" -- "$GRAPH_WARMTH" "$gate"
    watch_logs "$jid"

    sleep 2 # captured + instantiated, holding at the gate (pre-launch)

    cedana dump job "$jid"

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

# Several live graphs of one topology; the cold siblings launch for the first
# time only after restore (gate), the warm ones span the checkpoint.
# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph siblings)" {
    jid=$(unix_nano)
    gate=/tmp/gate-$jid
    rm -f "$gate"

    cedana run process -g --jid "$jid" -- "$GRAPH_SIBLINGS" "$gate"
    watch_logs "$jid"

    sleep 3 # warm siblings launching; cold ones held by the gate

    cedana dump job "$jid"

    cedana restore job "$jid"
    watch_logs "$jid"

    touch "$gate" # launch the cold siblings for the first time, post-restore
    sleep 2

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    rm -f "$gate"
    run cedana job kill "$jid"
}
