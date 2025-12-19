#!/bin/bash
set -e

# XXX: Hack - disable io_uring
# disable only if sysctl exists
if sysctl -a 2>/dev/null | grep -q kernel.io_uring_disabled; then
    sudo sysctl -w kernel.io_uring_disabled=2
fi
