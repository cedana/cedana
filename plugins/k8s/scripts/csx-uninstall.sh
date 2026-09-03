#!/bin/bash
set -euo pipefail

# If CSX was installed this script uninstalls it
export CSX_APP_NAME="csx"
export CSX_APP_PATH=/usr/local/bin/$CSX_APP_NAME
export CSX_LOG_PATH="/var/log/$CSX_APP_NAME-daemon.log"
export CSX_SERVICE_FILE="/etc/systemd/system/$CSX_APP_NAME.service"
export CSX_ENV_FILE="/etc/csx/csx.env"

# Use systemd only when it is running and the service file exists.
if [ -d /run/systemd/system ] && [ -f "$CSX_SERVICE_FILE" ]; then
    echo "Stopping $CSX_APP_NAME service..."
    systemctl disable --now "$CSX_APP_NAME.service" || true

    rm -f "$CSX_SERVICE_FILE"

    echo "Reloading systemd..."
    systemctl daemon-reload
else
    pkill -f "$CSX_APP_PATH" || true
    echo "No csx systemd service found. Killed any running processes just in case."
fi

rm -f "$CSX_LOG_PATH" "$CSX_APP_PATH" "$CSX_ENV_FILE"
rmdir "$(dirname "$CSX_ENV_FILE")" 2>/dev/null || true
echo "$CSX_APP_NAME uninstall complete."
