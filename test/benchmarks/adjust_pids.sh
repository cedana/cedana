#!/bin/bash

# Ensure we have root privileges
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root"
    exit 1
fi

# Target PID threshold
TARGET_PID=10000

adjust_pids() {
    current_pid=$(cat /proc/sys/kernel/ns_last_pid)
    if [ "$current_pid" -lt "$TARGET_PID" ]; then
        echo "Current PID ($current_pid) is less than $TARGET_PID, adjusting..."
        echo "$((TARGET_PID - 1))" >/proc/sys/kernel/ns_last_pid
        # Create dummy processes to reach the target PID
        while [ "$(cat /proc/sys/kernel/ns_last_pid)" -lt "$TARGET_PID" ]; do
            (sleep 0.01 &)
            wait $!
        done
        echo "PID adjusted to $(cat /proc/sys/kernel/ns_last_pid)"
    else
        echo "Current PID ($current_pid) is already greater than or equal to $TARGET_PID"
    fi
}

# Adjust PIDs
adjust_pids
