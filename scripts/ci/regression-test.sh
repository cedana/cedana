#!/bin/bash -e
# Used to run regression bats tests (located in tests/regression)

source ./helpers.sh
CEDANA_DIR=`pwd`../../

function start_regression() {
    echo "Running regression tests in cwd: $(pwd)"
    bats test/regression/main.bats
    echo "Regression tests complete with status: $?"
}

cleanup() {
    echo "Cleaning up..."
    cd $CEDANA_DIR
    stop_cedana
}
trap cleanup EXIT

main() {
    cd $CEDANA_DIR
    print_env
    source_env
    start_cedana
    start_regression
}

main
