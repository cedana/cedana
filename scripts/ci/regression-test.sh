#!/bin/bash -e
# Used to run regression bats tests (located in tests/regression)

source ./helpers.sh

function start_regression() {
    pushd test/regression && echo "Running regression tests in cwd: $(pwd)"
    bats main.bats
    popd
}

main() {
    pushd ../.. && echo "Starting regression tests in cwd: $(pwd)"
    print_env || { echo "print_env failed"; exit 1; }
    source_env || { echo "source_env failed"; exit 1; }
    start_cedana || { echo "start_cedana failed"; exit 1; }
    start_regression || { echo "start_regression failed"; exit 1; }
    stop_cedana || { echo "stop_cedana failed"; exit 1; }
    popd
}

main
