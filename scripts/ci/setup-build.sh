#!/bin/bash

source ./helpers.sh

main() {
    setup_ci_build || { echo "setup_ci_build failed"; exit 1; }
}

main
