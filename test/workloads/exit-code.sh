#!/bin/sh

# Exits with the given status code.

if [ "$#" -ne 1 ]; then
    exit 0
fi

exit "$1"
