#!/bin/sh

# Print the current date every second for a given number of seconds.

COUNT=${1:-180}

i=0
while :; do
    sleep 1
    date
    i=$((i + 1))
    [ $i -ge "$COUNT" ] && break
done
