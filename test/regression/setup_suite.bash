#!/bin/bash

# This file contains setup functions that run for the duration of the test suite run.

source "${BATS_TEST_DIRNAME}"/../helpers/utils.bash
source "${BATS_TEST_DIRNAME}"/../helpers/daemon.bash
source "${BATS_TEST_DIRNAME}"/../helpers/containerd.bash

setup_suite() {
    debug cedana plugin install criu@criu-dev
    start_containerd
}

teardown_suite() {
    stop_containerd
}
