#!/bin/bash

source ./helpers.sh

main() {
    pushd ../. && echo "Running setup_ci_build in $(pwd)" &&
    setup_ci_build || { echo "setup_ci_build failed"; exit 1; }
    popd
}

main
