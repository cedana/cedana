#!/bin/bash -e

set eux

# Used to run correctness tests for CI

source ./helpers.sh

start_correctness() {
    echo "Running correctness test in cwd: $(pwd)"
    sudo -E python3 test/benchmarks/performance.py --correctness --verbose
    if [[ $? -ne 0 ]]; then
        echo "start_correctness failed!"
        exit 1
    fi
}

main() {
    pushd ../..
    print_env
    source_env
    start_otelcol
    start_cedana
    start_correctness
    stop_cedana
    popd
}

main
