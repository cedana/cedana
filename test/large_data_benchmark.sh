#!/bin/bash

# Number of iterations
num_iterations=20

# Get the start time
start_time=$(date +%s)

# List of benchmarking subdirectories
subdirectories=("results" "pids" "processes" "temp")

# Function to create a directory if it doesn't exist
create_directory_if_not_exists() {
    if [ ! -d "$1" ]; then
        echo "Creating directory: $1"
        mkdir -p "$1"
    fi
}

# Check if benchmarking directory exists and create it if needed
create_directory_if_not_exists "benchmarking"

# Loop through the subdirectories and create them if needed
for subdir in "${subdirectories[@]}"; do
    create_directory_if_not_exists "benchmarking/$subdir"
done

# Loop through iterations
for ((i = 1; i <= num_iterations; i++)); do
    # Run your script in the background
    ./test/run_benchmarks.sh &

    # Store the process ID (PID) of the background process
    bg_pid=$!

    # Wait for the background process to finish
    wait "$bg_pid"

    echo "Iteration $i completed"
done

# Get the end time
end_time=$(date +%s)

# Calculate elapsed time
elapsed_time=$((end_time - start_time))

echo "All iterations completed"
echo "Elapsed time: $elapsed_time seconds"

python3 test/benchmark_analysis.py
