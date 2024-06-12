#!/bin/bash
# Used to run a quick smoke test for CI

source ./helpers.sh

start_smoke() {
    sudo -E python3 test/benchmarks/performance.py --smoke --num_samples 1
}

print_env
setup_ci
start_cedana
start_smoke
cleanup_cedana
