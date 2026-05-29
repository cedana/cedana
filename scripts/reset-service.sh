#!/bin/bash
set -euo pipefail

# Source utils.sh if running as a standalone script (BASH_SOURCE is set)
if [ -n "${BASH_SOURCE[0]:-}" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ -f "$SCRIPT_DIR/utils.sh" ]; then
        source "$SCRIPT_DIR/utils.sh"
    fi
fi

# check if systemd is available and running
if ! systemctl status &>/dev/null; then
    pkill -f "$APP_PATH daemon start" || true
    echo "No systemd. Killed any running processes, but no service to remove."
    exit
fi

if [ -f "$SERVICE_FILE" ]; then
    echo "Stopping $APP_NAME service..."
    # Try to stop service, use sudo if needed
    if ! systemctl stop "$APP_NAME".service 2>/dev/null; then
        if [ "$EUID" -eq 0 ]; then
            systemctl stop "$APP_NAME".service
        elif sudo -n true 2>/dev/null; then
            sudo systemctl stop "$APP_NAME".service
        elif [ -t 0 ]; then
            echo "Sudo access required to stop service"
            sudo systemctl stop "$APP_NAME".service || exit 1
        else
            if ! sudo systemctl stop "$APP_NAME".service 2>/dev/null; then
                echo "ERROR: Cannot stop service in non-interactive mode without passwordless sudo"
                exit 1
            fi
        fi
    fi

    # truncate the logs
    echo -n > "$LOG_PATH"

    # Remove service file, use sudo if needed
    if ! rm -f "$SERVICE_FILE" 2>/dev/null; then
        if [ "$EUID" -eq 0 ]; then
            rm -f "$SERVICE_FILE"
        elif sudo -n true 2>/dev/null; then
            sudo rm -f "$SERVICE_FILE"
        elif [ -t 0 ]; then
            echo "Sudo access required to remove service file"
            sudo rm -f "$SERVICE_FILE" || exit 1
        else
            if ! sudo rm -f "$SERVICE_FILE" 2>/dev/null; then
                echo "ERROR: Cannot remove service file in non-interactive mode without passwordless sudo"
                exit 1
            fi
        fi
    fi

    echo "Reloading systemd..."
    # Try to reload systemd, use sudo if needed
    if ! systemctl daemon-reload 2>/dev/null; then
        if [ "$EUID" -eq 0 ]; then
            systemctl daemon-reload
        elif sudo -n true 2>/dev/null; then
            sudo systemctl daemon-reload
        elif [ -t 0 ]; then
            echo "Sudo access required to reload systemd"
            sudo systemctl daemon-reload || exit 1
        else
            if ! sudo systemctl daemon-reload 2>/dev/null; then
                echo "ERROR: Cannot reload systemd in non-interactive mode without passwordless sudo"
                exit 1
            fi
        fi
    fi
else
    pkill -f "$APP_PATH daemon start" || true
    echo "No systemd service found, but killed any running processes just in case."
fi
