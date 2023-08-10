#!/bin/bash

# Set the path to your executable and directory
executable="benchmarking/processes/loop"
directory="benchmarking/pids"

# Check if the executable exists
if [ ! -f "$executable" ]; then
    echo "Executable not found: $executable"
    exit 1
fi

# Check if the directory exists
if [ ! -d "$directory" ]; then
    echo "Directory not found: $directory"
    exit 1
fi

# Remove all files in the directory
rm -f "$directory"/*
echo "All files in the directory have been removed."

# Run the executable in the background
"$executable" &

# Run tests
sudo /usr/local/go/bin/go test -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDump$ -parallel 4 github.com/nravic/cedana/cmd
