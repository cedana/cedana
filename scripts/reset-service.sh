#!/bin/bash
set -euo pipefail

# Source utils.sh if running as a standalone script (BASH_SOURCE is set)
if [ -n "${BASH_SOURCE[0]:-}" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ -f "$SCRIPT_DIR/utils.sh" ]; then
        source "$SCRIPT_DIR/utils.sh"
    fi
fi

check_root

# check if systemd is available and running
if ! systemctl status &>/dev/null; then
    pkill -f "$APP_PATH daemon start" || true
    echo "No systemd. Killed any running processes, but no service to remove."
    exit
fi

if [ -f "$SERVICE_FILE" ]; then
    echo "Stopping $APP_NAME service..."
    systemctl stop "$APP_NAME".service

    # truncate the logs
    echo -n > "$LOG_PATH"

    rm -f "$SERVICE_FILE"
else
    pkill -f "$APP_PATH daemon start" || true
    echo "No systemd service found, but killed any running processes just in case."
fi
