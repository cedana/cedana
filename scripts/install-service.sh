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

SERVICE_EXISTS=false
if test -f "$SERVICE_FILE"; then
    echo "Service file exists, will restart after update..."
    SERVICE_EXISTS=true
    # Stop the existing service first
    if ! systemctl stop "$APP_NAME".service 2>/dev/null; then
        if [ "$EUID" -eq 0 ]; then
            systemctl stop "$APP_NAME".service 2>/dev/null || true
        elif sudo -n true 2>/dev/null; then
            sudo systemctl stop "$APP_NAME".service 2>/dev/null || true
        elif [ -t 0 ]; then
            echo "Sudo access required to stop existing service"
            sudo systemctl stop "$APP_NAME".service || true
        fi
    fi
fi

echo "Creating $SERVICE_FILE..."

# Cedana daemon should run as root for checkpoint/restore operations
# But we need to ensure the binary is accessible
SERVICE_UID=0
SERVICE_GID=0

# If using a custom bin directory, ensure it's readable by root
if [ -n "${CEDANA_PLUGINS_BIN_DIR:-}" ] && [ "${CEDANA_PLUGINS_BIN_DIR}" != "/usr/local/bin" ]; then
    echo "Ensuring $APP_PATH is executable..."
    if [ -f "$APP_PATH" ]; then
        chmod +x "$APP_PATH" 2>/dev/null || sudo chmod +x "$APP_PATH"
    else
        echo "WARNING: Binary not found at $APP_PATH"
        echo "Make sure cedana is installed at: $APP_PATH"
    fi
fi

# Build the daemon command with optional config-dir
DAEMON_CMD="$APP_PATH daemon start"
if [ -n "${CEDANA_CONFIG_DIR:-}" ] && [ "${CEDANA_CONFIG_DIR}" != "/etc/cedana" ]; then
    # Expand tilde and make absolute path for systemd compatibility
    EXPANDED_CONFIG_DIR=$(eval echo "${CEDANA_CONFIG_DIR}")
    DAEMON_CMD="$DAEMON_CMD --config-dir=${EXPANDED_CONFIG_DIR}"
fi

# Define the service file content
SERVICE_CONTENT="[Unit]
Description=Cedana Daemon
[Service]
ExecStart=$DAEMON_CMD
Environment=CEDANA_CONFIG_DIR=${EXPANDED_CONFIG_DIR:-/etc/cedana}
User=$SERVICE_UID
Group=$SERVICE_GID
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

# Determine if we're updating or fresh install
if [ "$SERVICE_EXISTS" = true ]; then
    echo "Restarting $APP_NAME service with new configuration..."
else
    echo "Enabling and starting $APP_NAME service..."
fi

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

# Start or restart the service
if [ "$SERVICE_EXISTS" = true ]; then
    # Service exists, try reload-or-restart (restart if reload not supported)
    if ! systemctl reload-or-restart "$APP_NAME".service 2>/dev/null; then
        if [ "$EUID" -eq 0 ]; then
            systemctl reload-or-restart "$APP_NAME".service
        elif sudo -n true 2>/dev/null; then
            sudo systemctl reload-or-restart "$APP_NAME".service
        elif [ -t 0 ]; then
            echo "Sudo access required to restart service"
            sudo systemctl reload-or-restart "$APP_NAME".service || exit 1
        else
            if ! sudo systemctl reload-or-restart "$APP_NAME".service 2>/dev/null; then
                echo "ERROR: Cannot restart service in non-interactive mode without passwordless sudo"
                exit 1
            fi
        fi
    fi
else
    # Fresh install, just start
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
fi

echo "$APP_NAME service setup complete."

# Verify the service is running with correct configuration
echo "Service configured to run: $DAEMON_CMD"
if [ -n "${CEDANA_CONFIG_DIR:-}" ] && [ "${CEDANA_CONFIG_DIR}" != "/etc/cedana" ]; then
    echo "Using config directory: ${CEDANA_CONFIG_DIR}"
fi
