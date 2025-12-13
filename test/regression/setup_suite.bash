#!/bin/bash

# This file contains setup functions that run for the duration of the test suite run.

source "${BATS_TEST_DIRNAME}"/../helpers/utils.bash

setup_suite() {
    cedana plugin install criu
}
