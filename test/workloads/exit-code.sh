#!/bin/sh

# Exits with the given status code.

trap 'exit 1' INT TERM

if [ "$#" -ne 1 ]; then
    exit 0
fi

exit "$1"
