#!/bin/bash

source ./helpers.sh

main() {
    setup_ci || { echo "setup_ci failed"; exit 1; }
}

main
