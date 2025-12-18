#!/bin/sh

# Print the current date every second for a given number of seconds.

trap 'exit 1' INT TERM

COUNT=${1:-999}

i=0
while :; do
    sleep 1
    date
    i=$((i + 1))
    [ $i -ge "$COUNT" ] && break
done

if [ "$#" -ne 2 ]; then
    exit 0
fi

echo "$2"
exit "$2"
