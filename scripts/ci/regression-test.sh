#!/bin/bash -e
# Used to run regression bats tests (located in tests/regression)

source ./helpers.sh

function start_regression() {
    pushd test/regression && echo "Running regression tests in cwd: $(pwd)"
    bats main.bats
    popd
}

cleanup() {
    stop_cedana
}
trap cleanup EXIT

main() {
    pushd ../.. && echo "Starting regression tests in cwd: $(pwd)"
    print_env
    source_env
    start_cedana
    start_regression
    popd
}

main
