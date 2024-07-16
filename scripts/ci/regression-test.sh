#!/bin/bash -e
# Used to run regression bats tests (located in tests/regression)

source ./helpers.sh

function start_regression() {
    pushd test/regression && echo "Running regression tests in cwd: $(pwd)"
    bats main.bats
    echo "Regression tests complete with status: $?"
    popd
}

cleanup() {
    echo "Cleaning up..."
    pushd ../..
    stop_cedana
    popd
}
trap cleanup EXIT

main() {
    pushd ../..
    print_env
    source_env
    start_cedana
    start_regression
    stop_cedana
    popd
}

main
