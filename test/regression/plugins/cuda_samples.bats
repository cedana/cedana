#!/usr/bin/env bats

# Curated cuda-samples run natively + intercepted; fails only on regressions.
# Also tagged cuda-samples so the main gpu run can exclude it (!cuda-samples).
#
# bats file_tags=gpu,cuda-samples

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

CUDA_SAMPLES_DIR="/cedana-samples/gpu_smr/cuda-samples"

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    if [ ! -d "$CUDA_SAMPLES_DIR/bin" ]; then
        skip "cuda-samples not in image (rebuild cedana-samples image)"
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

cuda_sample_intercepted() {
    local sample="$1" bin="$CUDA_SAMPLES_DIR/bin/$1"

    [ -x "$bin" ] || skip "not in image"

    run "$bin"
    [ "$status" -eq 0 ] || skip "native rc=$status"

    run cedana run process --attach -g --jid "$(unix_nano)-$sample" -- "$bin"
    [ "$status" -eq 0 ] || fail "cedana-induced regression (intercepted rc=$status): $output"
}

# One test per sample, registered at load time since @test can't be looped.
# Dynamic tests don't inherit file_tags, so tag each explicitly.
mapfile -t CUDA_SAMPLES < <(sed 's/#.*//; s/[[:space:]]//g; /^$/d' "$CUDA_SAMPLES_DIR/samples.txt" 2>/dev/null)
if [ "${#CUDA_SAMPLES[@]}" -eq 0 ]; then
    bats_test_function --tags gpu,cuda-samples --description "[$GPU_INFO] cuda-samples (intercepted)" -- fail "no samples listed in $CUDA_SAMPLES_DIR/samples.txt"
fi
for sample in "${CUDA_SAMPLES[@]}"; do
    bats_test_function --tags gpu,cuda-samples --description "[$GPU_INFO] cuda-samples (intercepted): $sample" -- cuda_sample_intercepted "$sample"
done
