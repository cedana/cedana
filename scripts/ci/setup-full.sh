#!/bin/bash

source ./helpers.sh

main() {
    pushd ../.. && echo "Running setup_ci in $(pwd)" &&
    setup_ci || { echo "setup_ci failed"; exit 1; }
    popd
}

main
