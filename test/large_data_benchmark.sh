#!/bin/bash

# Number of iterations
num_iterations=100

# Get the start time
start_time=$(date +%s)

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
