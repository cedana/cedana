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
    echo "Systemd not available. Starting cedana daemon directly without service setup..." >&2
    $APP_PATH daemon start &>/var/log/cedana-daemon.log &
    exit
fi

if test -f "$SERVICE_FILE"; then
    echo "Restarting $APP_NAME..."
fi

echo "Creating $SERVICE_FILE..."
cat <<EOF | $SUDO_USE tee "$SERVICE_FILE" >/dev/null
[Unit]
Description=Cedana Daemon
[Service]
ExecStart=$APP_PATH daemon start $DAEMON_ARGS
User=root
Group=root
Restart=no

[Install]
WantedBy=multi-user.target

[Service]
StandardError=append:/var/log/cedana-daemon.log
StandardOutput=append:/var/log/cedana-daemon.log
EOF

echo "Reloading systemd..."
$SUDO_USE systemctl daemon-reload

echo "Enabling and starting $APP_NAME service..."
$SUDO_USE systemctl enable "$APP_NAME".service
$SUDO_USE systemctl start "$APP_NAME".service
echo "$APP_NAME service setup complete."
