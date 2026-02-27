#!/bin/bash
set -euo pipefail

# Source utils.sh if running as a standalone script (BASH_SOURCE is set)
if [ -n "${BASH_SOURCE[0]:-}" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ -f "$SCRIPT_DIR/utils.sh" ]; then
        source "$SCRIPT_DIR/utils.sh"
    fi
fi

if [ "${DISABLE_IO_URING:-false}" = "true" ]; then
    echo "Attempting to disable IO Uring using sysctl..."
    if sysctl -a 2>/dev/null | grep -q kernel.io_uring_disabled; then
        echo "Found kernel.io_uring_disabled sysctl parameter"
        if sudo sysctl -w kernel.io_uring_disabled=2; then
            echo "IO Uring disabled successfully (kernel.io_uring_disabled=2)"
        else
            echo "ERROR: Failed to set kernel.io_uring_disabled=2 (sysctl command failed)"
        fi
    else
        echo "ERROR: Cannot disable IO Uring - kernel.io_uring_disabled sysctl parameter not found"
        echo "This kernel may not support io_uring or the parameter is not available"
    fi
fi
