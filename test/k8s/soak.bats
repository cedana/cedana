#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,soak

# Soak tests: the same pod checkpoint/restore many times, to catch failures that only
# happen 1 in N times (races). They run nightly (the soak tag), counting every failure, so
# the result is a failure rate rather than the first failure. Each cycle gets every check
# test_pod_spec makes (restore status, DNS, hostname, token, count continuity).

load ../helpers/utils
load ../helpers/daemon
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

# A pod cycle takes ~45s on CPU and ~95s on a GPU
SOAK_ITERATIONS=${SOAK_ITERATIONS:-50}
SOAK_GPU_ITERATIONS=${SOAK_GPU_ITERATIONS:-20}

soak() {
    local iterations="$1"
    shift

    local i failures=0 first_failure="" first_output=""
    for i in $(seq 1 "$iterations"); do
        local spec output
        spec=$("$@")
        if ! output=$(test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 300 5 120 2>&1); then
            failures=$((failures + 1))
            if [ -z "$first_failure" ]; then
                first_failure=$i
                first_output=$(tail -n 40 <<<"$output")
            fi
        fi
    done
    if [ "$failures" -gt 0 ]; then
        error_log "$failures of $iterations pod dump/restore cycles failed (first: cycle $first_failure, its end below)"
        echo "$first_output"
        return 1
    fi
}

cpu_spec() {
    cmd_pod_spec "alpine:latest" "$(cat "$WORKLOADS"/count-loop.sh)"
}

gpu_spec() {
    cmd_pod_spec_gpu "$GPU_WORKLOAD_IMAGE" '/cedana-samples/gpu_smr/mem-throughput-saxpy-loop' 1
}

# bats test_tags=dump,restore
@test "soak: dump and restore a pod, $SOAK_ITERATIONS times" {
    soak "$SOAK_ITERATIONS" cpu_spec
}

# bats test_tags=dump,restore,gpu
@test "soak: dump and restore a GPU pod, $SOAK_GPU_ITERATIONS times" {
    soak "$SOAK_GPU_ITERATIONS" gpu_spec
}
