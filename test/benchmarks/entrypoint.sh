#!/bin/bash 

# entrypoint for benchmarking docker script
./reset.sh

# start otelcol polling
otelcol-contrib --config test/benchmarks/local-otelcol-config.yaml &

# start daemon
./build-start-daemon.sh

# start benchmarking, pass all args
sudo -E python3 test/benchmarks/performance.py "$@"

