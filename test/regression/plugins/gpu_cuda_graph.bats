#!/usr/bin/env bats

# CUDA graph C/R, templating OFF (default restore path). Same scenarios run with
# templating on in gpu_cuda_graph_templates.bats; shared bodies in helpers/gpu.bash.
# Skips if the cuda_graph_* samples aren't in the image (like cuda_samples.bats).
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

    wait_for_graph_log "$jid" 'iter=3 '
    graph_no_mismatch "$jid"

    run cedana job kill "$jid"
}

###############
### Restore ###
###############

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph capture loop)" {
    graph_scenario_basic "$GRAPH_LOOP"
}

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph events + pool churn)" {
    graph_scenario_basic "$GRAPH_EVENTS"
}

# bats test_tags=restore,crcr
@test "[$GPU_INFO] restore->dump->restore GPU process (cuda graph events)" {
    graph_scenario_crcr "$GRAPH_EVENTS"
}

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph, cold checkpoint / unlaunched)" {
    graph_scenario_cold "$GRAPH_WARMTH"
}

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph, warm checkpoint / built-up state)" {
    graph_scenario_warm "$GRAPH_WARMTH"
}

# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (cuda graph siblings, mixed warm/cold)" {
    graph_scenario_siblings "$GRAPH_SIBLINGS"
}
