#!/bin/sh

# Print an increasing count every second, so a restore can tell it continued (k8s tests
# check the restored count is at least the one at checkpoint).

trap 'exit 0' INT TERM

i=0
while :; do
    echo "COUNT $i"
    i=$((i + 1))
    sleep 1
done
