#!/bin/bash 

# entrypoint for benchmarking docker script 

# start otelcol polling 
otelcol-contrib --config test/benchmarks/otelcol-config.yaml &

# start daemon 
sudo -E ./cedana daemon start & 

# start benchmarking
sudo -E python3 test/benchmarks/benchmark.py