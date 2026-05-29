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

# Get the current user's primary group ID
CURRENT_GID=$(id -g)

# Build the daemon command with optional config-dir
DAEMON_CMD="$APP_PATH daemon start"
if [ -n "${CEDANA_CONFIG_DIR:-}" ] && [ "${CEDANA_CONFIG_DIR}" != "/etc/cedana" ]; then
    DAEMON_CMD="$DAEMON_CMD --config-dir=${CEDANA_CONFIG_DIR}"
fi

# Define the service file content
SERVICE_CONTENT="[Unit]
Description=Cedana Daemon
[Service]
ExecStart=$DAEMON_CMD
Environment=CEDANA_CONFIG_DIR=${CEDANA_CONFIG_DIR:-/etc/cedana}
User=$UID
Group=$CURRENT_GID
Restart=no

[Install]
WantedBy=multi-user.target

[Service]
StandardError=append:$LOG_PATH
StandardOutput=append:$LOG_PATH"

# Try to create service file, use sudo if needed
if echo "$SERVICE_CONTENT" > "$SERVICE_FILE" 2>/dev/null; then
    : # Successfully created file
elif [ "$EUID" -eq 0 ]; then
    echo "$SERVICE_CONTENT" > "$SERVICE_FILE"
elif sudo -n true 2>/dev/null; then
    echo "$SERVICE_CONTENT" | sudo tee "$SERVICE_FILE" >/dev/null
elif [ -t 0 ]; then
    echo "Requesting sudo access to create service file..."
    echo "$SERVICE_CONTENT" | sudo -S tee "$SERVICE_FILE" >/dev/null
else
    echo "ERROR: Cannot create service file without sudo access"
    echo "Please run with sudo or grant appropriate permissions"
    exit 1
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

echo "Enabling and starting $APP_NAME service..."
# Try to enable and start service, use sudo if needed
if ! systemctl enable "$APP_NAME".service 2>/dev/null; then
    if [ "$EUID" -eq 0 ]; then
        systemctl enable "$APP_NAME".service
    elif sudo -n true 2>/dev/null; then
        sudo systemctl enable "$APP_NAME".service
    elif [ -t 0 ]; then
        echo "Sudo access required to enable service"
        sudo systemctl enable "$APP_NAME".service || exit 1
    else
        if ! sudo systemctl enable "$APP_NAME".service 2>/dev/null; then
            echo "ERROR: Cannot enable service in non-interactive mode without passwordless sudo"
            exit 1
        fi
    fi
fi

if ! systemctl start "$APP_NAME".service 2>/dev/null; then
    if [ "$EUID" -eq 0 ]; then
        systemctl start "$APP_NAME".service
    elif sudo -n true 2>/dev/null; then
        sudo systemctl start "$APP_NAME".service
    elif [ -t 0 ]; then
        echo "Sudo access required to start service"
        sudo systemctl start "$APP_NAME".service || exit 1
    else
        if ! sudo systemctl start "$APP_NAME".service 2>/dev/null; then
            echo "ERROR: Cannot start service in non-interactive mode without passwordless sudo"
            exit 1
        fi
    fi
fi

echo "$APP_NAME service setup complete."
