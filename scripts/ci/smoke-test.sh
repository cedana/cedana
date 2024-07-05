#!/bin/bash
# Used to run a quick smoke test for CI

source ./helpers.sh

start_smoke() {
    sudo -E python3 test/benchmarks/performance.py --smoke --num_samples 1
    if [[ $? -ne 0 ]]; then
        echo "start_smoke failed!"
        exit 1
    fi
}

main() {
    pushd ../.. && echo "Starting smoke test in cwd: $(pwd)"
    print_env || { echo "print_env failed"; exit 1; }
    setup_ci || { echo "setup_ci failed"; exit 1; }
    source_env || { echo "source_env failed"; exit 1; }
    start_cedana || { echo "start_cedana failed"; exit 1; }
    start_smoke || { echo "start_smoke failed"; exit 1; }
    stop_cedana || { echo "stop_cedana failed"; exit 1; }
    popd
}

main
