#!/bin/bash

set -eo pipefail

# get the directory of the script
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE" ]; do
    DIR="$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)"
    SOURCE="$(readlink "$SOURCE")"
    [[ $SOURCE != /* ]] && SOURCE="$DIR/$SOURCE"
done
DIR="$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)"

source "$DIR"/utils.sh

DAEMON_ARGS=""

if ! test -f "$APP_PATH"; then
    echo "No binary found. Have you run make install?" >&2
    exit 1
fi

# check if systemctl is available
if ! command -v systemctl &>/dev/null; then
    echo "No systemctl on this system." >&2
    exit 1
fi

for arg in "$@"; do
    if [[ $arg == --args=* ]]; then
        value="${arg#*=}"
        echo "Daemon args: $value"
        DAEMON_ARGS="$value"
    fi
    if [ "$CEDANA_METRICS" == "true" ]; then
        echo "Metrics enabled..."
    fi
done

if test -f "$SERVICE_FILE"; then
    echo "Restarting $APP_NAME..."
fi

echo "Creating $SERVICE_FILE..."
cat <<EOF | $SUDO_USE tee "$SERVICE_FILE" >/dev/null
[Unit]
Description=Cedana Checkpointing Daemon
[Service]
Environment=USER=$USER
Environment=PATH=/cedana/bin:$PATH
Environment=CEDANA_LOG_LEVEL="$CEDANA_LOG_LEVEL"
Environment=CEDANA_LOG_LEVEL_NO_SERVER="$CEDANA_LOG_LEVEL_NO_SERVER"
Environment=CEDANA_URL="$CEDANA_URL"
Environment=CEDANA_AUTH_TOKEN="$CEDANA_AUTH_TOKEN"
Environment=CEDANA_ADDRESS="$CEDANA_ADDRESS"
Environment=CEDANA_PROTOCOL="$CEDANA_PROTOCOL"
Environment=CEDANA_DB_REMOTE="$CEDANA_DB_REMOTE"
Environment=CEDANA_CLIENT_WAIT_FOR_READY="$CEDANA_CLIENT_WAIT_FOR_READY"
Environment=CEDANA_PROFILING_ENABLED="$CEDANA_PROFILING_ENABLED"
Environment=CEDANA_METRICS="$CEDANA_METRICS"
Environment=CEDANA_CHECKPOINT_DIR="$CEDANA_CHECKPOINT_DIR"
Environment=CEDANA_CHECKPOINT_STREAMS="$CEDANA_CHECKPOINT_STREAMS"
Environment=CEDANA_CHECKPOINT_COMPRESSION="$CEDANA_CHECKPOINT_COMPRESSION"
Environment=CEDANA_GPU_POOL_SIZE="$CEDANA_GPU_POOL_SIZE"
Environment=CEDANA_GPU_FREEZE_TYPE="$CEDANA_GPU_FREEZE_TYPE"
Environment=CEDANA_GPU_SHM_SIZE="$CEDANA_GPU_SHM_SIZE"
Environment=CEDANA_GPU_LD_LIB_PATH="$CEDANA_GPU_LD_LIB_PATH"
Environment=CEDANA_CRIU_MANAGE_CGROUPS="$CEDANA_CRIU_MANAGE_CGROUPS"
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
