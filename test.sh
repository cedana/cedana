#!/bin/bash

# Your counting logic here (for example, counting from 1 to 10)
for i in {1..10}; do
    echo "Count: $i"
    sleep 1  # Adjust the sleep duration as needed
done &

# Print the PID of the background process
echo "PID of the background process: $!"

