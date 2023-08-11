#!/bin/bash

# Set the path to your execLoop and dirPids
execLoop="benchmarking/processes/loop"
execServer="benchmarking/processes/server"
dirPids="benchmarking/pids"

# Check if the execLoop exists
if [ ! -f "$execLoop" ]; then
    echo "Loop program not found: $execLoop"
    exit 1
fi

# Check if the dirPids exists
if [ ! -d "$dirPids" ]; then
    echo "benchmarking/pids not found: $dirPids"
    exit 1
fi

# Remove all files in the dirPids
rm -f "$dirPids"/*
echo "All files in the benchmarking/pids directory have been removed."

# Run the execLoop in the background
"$execLoop" &

# Run tests
sudo /usr/local/go/bin/go test -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpLoop$ github.com/nravic/cedana/cmd && \

# Remove all files in the dirPids
rm -f "$dirPids"/*
echo "All files in the benchmarking/pids directory have been removed."

# Check if the execServer exists
if [ ! -f "$execServer" ]; then
    echo "Server program not found: $execServer"
    exit 1
fi
# Run the execServer in the background
"$execServer" &
sudo /usr/local/go/bin/go test -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpServer$ github.com/nravic/cedana/cmd
