#!/bin/bash

# Set the path to your execLoop and dirPids
execLoop="benchmarking/processes/loop"
execServer="benchmarking/processes/server"
execPing="benchmarking/processes/ping"

dirPids="benchmarking/pids"
dirResults="benchmarking/results"
dirTempLoop="benchmarking/temp/loop"
dirTempServer="benchmarking/temp/server"
dirTempPytorch="benchmarking/temp/pytorch"

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
rm -f "$dirResults"/*
rm -f "$dirPids"/*
echo "All files in the benchmarking/pids directory have been removed."

# Run the execLoop in the background
"$execLoop" &

# Run tests
sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpLoop$ github.com/cedana/cedana/cmd && \

# Remove all files in the dirPids
rm -f "$dirPids"/*
rm -f "$dirResults"/*
echo "All files in the benchmarking/pids directory have been removed."

# Check if the execServer exists
if [ ! -f "$execServer" ]; then
    echo "Server program not found: $execServer"
    exit 1
fi
# Run the execServer in the background
"$execServer" &
sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpServer$ github.com/cedana/cedana/cmd && \

# Remove all files in the dirPids
rm -f "$dirPids"/*
rm -f "$dirResults"/*
echo "All files in the benchmarking/pids directory have been removed."

python3 benchmarking/processes/time_sequence_prediction/generate_sine_wave.py && \
python3 benchmarking/processes/time_sequence_prediction/train.py & \

sleep 15 && \

sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpPytorch$ github.com/cedana/cedana/cmd

sudo rm -rf "$dirTempLoop"/_home*
sudo rm -rf "$dirTempServer"/_home*
sudo rm -rf "$dirTempPytorch"/_home*
sudo rm -rf "$dirTempPytorch"/_usr*


# Dump fails on this program for some reason
# # Check if the execServer exists
# if [ ! -f "$execPing" ]; then
#     echo "Server program not found: $execPing"
#     exit 1
# fi
# # Run the execServer in the background
# "$execPing" &
# sudo /usr/local/go/bin/go test -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/server_memory.prof.gz -run=^$ -bench ^BenchmarkDumpPing$ github.com/cedana/cedana/cmd
