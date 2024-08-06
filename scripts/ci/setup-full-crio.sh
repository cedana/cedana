#!/bin/bash

source ./helpers.sh

main() {
    pushd ../.. && echo "Running setup_ci in $(pwd)" &&
    setup_ci_crio || { echo "setup_ci_crio failed"; exit 1; }
    popd
}

main
