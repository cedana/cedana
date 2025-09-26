#!/bin/bash

# This file contains setup functions that run for the duration of the test suite run.

setup_suite() {
    cedana plugin install criu
    entrypoint.sh # XXX: start docker-in-docker, from the container image
}
