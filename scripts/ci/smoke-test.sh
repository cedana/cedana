#!/bin/bash -e
# Used to run a quick smoke test for CI

source ./helpers.sh

start_smoke() {
    echo "Running smoke test in cwd: $(pwd)"
    sudo -E python3 test/benchmarks/performance.py --smoke --num_samples 1
    if [[ $? -ne 0 ]]; then
        echo "start_smoke failed!"
        exit 1
    fi
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
    setup_ci
    source_env
    start_cedana
    start_smoke
    popd
}

main
