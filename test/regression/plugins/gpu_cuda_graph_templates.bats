#!/usr/bin/env bats

# gpu_cuda_graph.bats scenarios with template-based restore on. Skips if the
# samples image predates the binaries.
#
# bats file_tags=gpu

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget
export CEDANA_GPU_TEMPLATES_ENABLED=true  # engage the template-based restore path

GRAPH_WARMTH=/cedana-samples/gpu_smr/cuda_graph_warmth
GRAPH_SIBLINGS=/cedana-samples/gpu_smr/cuda_graph_siblings

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    for bin in "$GRAPH_WARMTH" "$GRAPH_SIBLINGS"; do
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

# Warm: launched before dump -> EAGER bucket.
# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph, warm checkpoint) [templated]" {
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

# Cold: unlaunched at dump (gate holds first launch until post-restore) -> DEFERRED.
# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph, cold checkpoint / unlaunched) [templated]" {
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

# Siblings: same-topology graphs at mixed warmth -> EAGER + DEFERRED.
# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph siblings) [templated]" {
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
