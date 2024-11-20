#!/bin/bash

source ./helpers.sh

main() {
    echo "setup-build.sh: ls /usr/bin/cedana-image-streamer"
    ls /usr/bin/cedana-image-streamer
    pushd ../.. && echo "Running setup_ci_build in $(pwd)" &&
    setup_ci_build || { echo "setup_ci_build failed"; exit 1; }
    popd
}

main
