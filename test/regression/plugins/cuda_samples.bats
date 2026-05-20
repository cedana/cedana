#!/usr/bin/env bats

# Interception smoke test: runs the upstream NVIDIA cuda-samples suite via
# run_tests.py underneath cedana's GPU interceptor. Exercises a wide surface of
# CUDA driver/runtime APIs in one shot. Intended for the GPU runner.
#
# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu,cuda-samples

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

SAMPLES_SCRIPT="/cedana-samples/scripts/run-cuda-samples-tests.sh"

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    if [ ! -x "$SAMPLES_SCRIPT" ]; then
        skip "cuda-samples smoke-test script missing at $SAMPLES_SCRIPT (rebuild cedana-samples image)"
    fi
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

# bats test_tags=cuda-samples
@test "[$GPU_INFO] cuda-samples smoke test (intercepted)" {
    jid=$(unix_nano)

    debug cedana run process --attach -g --jid "$jid" \
        -- "$SAMPLES_SCRIPT"
}
