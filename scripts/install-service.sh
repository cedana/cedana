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
    # In the host's mount namespace, as a service would be: started from a pod (e.g. the k8s
    # helper), the pod's wouldn't see later mounts, e.g. new containers' rootfs (RW layers).
    if [ "$(readlink /proc/1/ns/mnt)" != "$(readlink /proc/self/ns/mnt)" ] && command -v nsenter &>/dev/null; then
        nsenter -t 1 -m -- "$APP_PATH" daemon start &> "$LOG_PATH" &
    else
        $APP_PATH daemon start &> "$LOG_PATH" &
    fi
    exit
fi

if test -f "$SERVICE_FILE"; then
    echo "Restarting $APP_NAME..."
fi

echo "Creating $SERVICE_FILE..."
POD_IDENTITY_ENV=""
if [ -n "${AWS_CONTAINER_CREDENTIALS_FULL_URI:-}" ] && [ -n "${AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE:-}" ]; then
    POD_IDENTITY_ENV=$(cat <<EOF
Environment="AWS_CONTAINER_CREDENTIALS_FULL_URI=${AWS_CONTAINER_CREDENTIALS_FULL_URI}"
Environment="AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE=${AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE}"
EOF
)
fi
CONTAINERD_ENV=""
if [ -n "${CONTAINERD_ADDRESS:-}" ]; then
    CONTAINERD_ENV="Environment=\"CONTAINERD_ADDRESS=${CONTAINERD_ADDRESS}\""
fi
cat <<EOF | tee "$SERVICE_FILE" >/dev/null
[Unit]
Description=Cedana Daemon
[Service]
ExecStart=$APP_PATH daemon start
User=root
Group=root
Restart=no
$POD_IDENTITY_ENV
$CONTAINERD_ENV

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
