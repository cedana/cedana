#!/bin/bash

# Set the path to your execLoop and dirPids
execLoop="benchmarking/processes/loop"
execServer="benchmarking/processes/server"
dirPids="benchmarking/pids"

# Check if the execLoop exists
if [ ! -f "$execLoop" ]; then
    echo "execLoop not found: $execLoop"
    exit 1
fi
# Check if the execServer exists
if [ ! -f "$execServer" ]; then
    echo "execServer not found: $execServer"
    exit 1
fi

# Check if the dirPids exists
if [ ! -d "$dirPids" ]; then
    echo "dirPids not found: $dirPids"
    exit 1
fi

# Remove all files in the dirPids
rm -f "$dirPids"/*
echo "All files in the dirPids have been removed."

# Run the execLoop in the background
"$execLoop" &
# Run the execServer in the background
"$execServer" &

# Run tests
sudo /usr/local/go/bin/go test -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpLoop$ github.com/nravic/cedana/cmd
wait
sudo /usr/local/go/bin/go test -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpServer$ github.com/nravic/cedana/cmd
