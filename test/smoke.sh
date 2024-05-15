#!/usr/bin/env bats

# setup cedana daemon
setup() {
    # start cedana daemon
    cedana daemon start &
}
