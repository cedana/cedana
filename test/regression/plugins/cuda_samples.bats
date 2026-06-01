#!/usr/bin/env bats

# Differential interception test for the NVIDIA cuda-samples suite.

# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu,cuda-samples

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

SAMPLES_DIR="/cedana-samples/gpu_smr/cuda-samples"
SAMPLES_SCRIPT="$SAMPLES_DIR/run-tests.sh"
COMPARE_SCRIPT="$SAMPLES_DIR/compare-results.py"

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    if [ ! -x "$SAMPLES_SCRIPT" ] || [ ! -f "$COMPARE_SCRIPT" ]; then
        skip "cuda-samples scripts missing under $SAMPLES_DIR (rebuild cedana-samples image)"
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
@test "[$GPU_INFO] cuda-samples differential (native baseline vs intercepted)" {
    native_jid=$(unix_nano)
    cedana_jid=$(unix_nano)
    native_out="/tmp/cuda-samples-native-$native_jid"
    cedana_out="/tmp/cuda-samples-cedana-$cedana_jid"

    # Baseline
    run cedana run process --attach --jid "$native_jid" \
        -- bash -c "OUTPUT_DIR='$native_out' '$SAMPLES_SCRIPT'"
    echo "native run rc=$status"
    echo "$output"

    # Candidate
    run cedana run process --attach -g --jid "$cedana_jid" \
        -- bash -c "OUTPUT_DIR='$cedana_out' '$SAMPLES_SCRIPT'"
    echo "intercepted run rc=$status"
    echo "$output"

    assert_exists "$native_out/results.json"
    assert_exists "$cedana_out/results.json"

    # Compare 
    run python3 "$COMPARE_SCRIPT" \
        --baseline "$native_out/results.json" \
        --candidate "$cedana_out/results.json"
    echo "$output"
    assert_success
}
