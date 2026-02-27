#!/bin/bash
set -euo pipefail

# Source utils.sh if running as a standalone script (BASH_SOURCE is set)
if [ -n "${BASH_SOURCE[0]:-}" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ -f "$SCRIPT_DIR/utils.sh" ]; then
        source "$SCRIPT_DIR/utils.sh"
    fi
fi

if ! test -f "$APP_PATH"; then
    echo "No binary found" >&2
    exit 1
fi

# check if systemd is available and running
if ! systemctl status &>/dev/null; then
    echo "Systemd not available. Starting $APP_NAME daemon directly without service setup..." >&2
    $APP_PATH daemon start &> "$LOG_PATH" &
    exit
fi

if test -f "$SERVICE_FILE"; then
    echo "Restarting $APP_NAME..."
fi

echo "Creating $SERVICE_FILE..."
cat <<EOF | tee "$SERVICE_FILE" >/dev/null
[Unit]
Description=Cedana Daemon
[Service]
ExecStart=$APP_PATH daemon start
User=root
Group=root
Restart=no

[Install]
WantedBy=multi-user.target

[Service]
StandardError=append:$LOG_PATH
StandardOutput=append:$LOG_PATH
EOF

echo "Reloading systemd..."
systemctl daemon-reload

echo "Enabling and starting $APP_NAME service..."
systemctl enable "$APP_NAME".service
systemctl start "$APP_NAME".service
echo "$APP_NAME service setup complete."
