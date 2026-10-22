#!/bin/bash

# This file contains setup functions that run for the duration of the test suite run.

source "${BATS_TEST_DIRNAME}"/../helpers/utils.bash
source "${BATS_TEST_DIRNAME}"/../helpers/daemon.bash
source "${BATS_TEST_DIRNAME}"/../helpers/containerd.bash
source "${BATS_TEST_DIRNAME}"/../helpers/metrics.bash

setup_suite() {
    check_env CEDANA_URL
    check_env CEDANA_AUTH_TOKEN

    info_log "====================================="
    info_log "Logs for this host can be viewed at:"
    info log_url_host "$CEDANA_URL"
    info_log "====================================="

    if command -v nvidia-ctk &>/dev/null && [ -f /.dockerenv  ]; then
        nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
    fi

    debug cedana plugin install criu@criu-dev
    start_containerd

    gpu_shm_segments >"$GPU_SHM_BEFORE"
}

teardown_suite() {
    stop_containerd

    check_gpu_shm_leaks
}

# GPU controllers' shared memory (/dev/shm/cedana-gpu.*), sized in GiBs. Each must be gone
# once its controller is: by the end of the suite every daemon is stopped, so any new one
# left is a leak, which piles up on a node until /dev/shm is full.
GPU_SHM_BEFORE=/tmp/cedana-gpu-shm-before

gpu_shm_segments() {
    find /dev/shm -maxdepth 1 -name 'cedana-gpu.*' -printf '%f\n' 2>/dev/null | sort
}

check_gpu_shm_leaks() {
    local leaked
    # Controllers exit asynchronously once their daemon stops
    for _ in $(seq 1 15); do
        leaked=$(comm -13 "$GPU_SHM_BEFORE" <(gpu_shm_segments) 2>/dev/null)
        [ -z "$leaked" ] && return 0
        sleep 1
    done
    local segment id log detail=""
    for segment in $leaked; do
        detail+="/dev/shm/$segment ($(du -h --apparent-size "/dev/shm/$segment" 2>/dev/null | cut -f1)), held by: $(fuser "/dev/shm/$segment" 2>/dev/null || echo none)"$'\n'
        # Which daemon (so test) spawned its controller, and the controller's last words
        id=${segment#cedana-gpu.}
        for log in $(grep -l "ID=$id" /tmp/cedana-daemon-*.log 2>/dev/null); do
            detail+="  spawned by the daemon logging to $log:"$'\n'
            detail+=$(grep -E "ID=$id|JID=" "$log" | grep -E "ID=$id|managing job|job exited" | tail -n 8 | sed 's/^/    /')$'\n'
        done
    done
    report_warning "GPU shared memory left behind after the suite (leaked): $(wc -w <<<"$leaked") segment(s)" "${detail%$'\n'}"
    return 1
}
