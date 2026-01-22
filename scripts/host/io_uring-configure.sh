#!/bin/bash
set -e

# XXX: Hack - disable io_uring
# disable only if sysctl exists
if [ "$DISABLE_IO_URING" = "true" ]; then
    echo "Disabling IO Uring using sysctl"
    if sysctl -a 2>/dev/null | grep -q kernel.io_uring_disabled; then
        sudo sysctl -w kernel.io_uring_disabled=2
        echo "IO Uring disabled successfully"
    else
        echo "Failed to disable IO Uring"
    fi
fi
