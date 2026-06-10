#!/usr/bin/env bats

# Curated cuda-samples run natively + intercepted; fails only on regressions.
# Tagged cuda-samples (not gpu) so the main gpu run skips it.
#
# bats file_tags=cuda-samples

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

# bats test_tags=cuda-samples
@test "[$GPU_INFO] cuda-samples (intercepted)" {
    local bin_dir="$CUDA_SAMPLES_DIR/bin"
    local regressions=() compared=0 skipped=0 line sample bin
    local -a lines

    # Array, not `while read < file`: cedana --attach would drain the list on stdin.
    mapfile -t lines <"$CUDA_SAMPLES_DIR/samples.txt"

    for line in "${lines[@]}"; do
        sample="${line%%#*}"
        sample="${sample//[[:space:]]/}"
        [ -z "$sample" ] && continue

        bin="$bin_dir/$sample"
        if [ ! -x "$bin" ]; then
            echo "skip $sample (not in image)"
            skipped=$((skipped + 1))
            continue
        fi

        run "$bin"
        if [ "$status" -ne 0 ]; then
            echo "skip $sample (native rc=$status)"
            skipped=$((skipped + 1))
            continue
        fi

        run cedana run process --attach -g --jid "$(unix_nano)" -- "$bin"
        compared=$((compared + 1))
        if [ "$status" -eq 0 ]; then
            echo "ok $sample"
        else
            echo "FAIL $sample (intercepted rc=$status)"
            echo "$output"
            regressions+=("$sample")
        fi
    done

    echo "compared=$compared skipped=$skipped regressions=${#regressions[@]}"
    [ "$compared" -gt 0 ] || fail "no cuda-samples ran intercepted (check $bin_dir / native failures)"
    [ "${#regressions[@]}" -eq 0 ] || fail "cedana-induced regressions: ${regressions[*]}"
}
