#!/bin/bash

# This file contains setup functions that run for the duration of the test suite run.

source "${BATS_TEST_DIRNAME}"/../helpers/utils.bash
source "${BATS_TEST_DIRNAME}"/../helpers/k8s.bash
source "${BATS_TEST_DIRNAME}"/../helpers/helm.bash
source "${BATS_TEST_DIRNAME}"/../helpers/containerd.bash

setup_suite() {
    install_kubectl
    install_helm
    install_k9s
    start_containerd
}

teardown_suite() {
    stop_containerd
}
