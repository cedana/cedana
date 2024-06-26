#!/bin/bash

# Used to run regression bats tests (located in tests/regression)

source ./helpers.sh

function start_regression() {
    echo "Running regression tests in cwd: $(pwd)"
    cd test/regression
    bats main.bats
    cd -
}

main() {
    print_env || { echo "print_env failed"; exit 1; }
    start_regression || { echo "start_regression failed"; exit 1; }
    stop_cedana || { echo "stop_cedana failed"; exit 1; }
}

main
