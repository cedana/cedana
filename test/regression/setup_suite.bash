#!/bin/bash

# This file contains setup functions that run for the duration of the test suite run.

source "${BATS_TEST_DIRNAME}"/../helpers/utils.bash
source "${BATS_TEST_DIRNAME}"/../helpers/daemon.bash
source "${BATS_TEST_DIRNAME}"/../helpers/containerd.bash
source "${BATS_TEST_DIRNAME}"/../helpers/metrics.bash

setup_suite() {
    info_log "Logs for this host can be viewed at:"
    info log_url_host "$CEDANA_URL"

    debug cedana plugin install criu@criu-dev
    start_containerd
}

teardown_suite() {
    stop_containerd
}
