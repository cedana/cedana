#!/bin/bash

# This is a helper file assumes its users are in the same directory as the Makefile

# crun containers use the same OCI bundles as runc, so all the bundle helpers
# are shared with the runc helper. Only the container status helpers differ,
# as they use the crun binary (overridden below).

source "$(dirname "${BASH_SOURCE[0]}")/runc.bash"

container_status() {
    local cid="$1"
    crun list | awk -v id="$cid" 'NR>1 && $1==id {print $3}'
}

container_pid() {
    local cid="$1"
    crun list | awk -v id="$cid" 'NR>1 && $1==id {print $2}'
}
