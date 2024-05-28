#!/bin/bash

# Used to run regression bats tests (located in tests/regression)

source ./helpers.sh

function start_regression() {
    echo "Running regression tests in cwd: $(pwd)"
    cd tests/regression
    bats main.bats
}

# we assume setup_ci and start_cedana have checked out
# the correct branch, and we're currently cd'd into the right dir
print_env
setup_ci
start_cedana
start_regression
