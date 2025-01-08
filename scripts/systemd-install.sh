#!/bin/bash

set -e

# get the directory of the script
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE"  ]; do
    DIR="$( cd -P "$( dirname "$SOURCE"  )" >/dev/null 2>&1 && pwd  )"
    SOURCE="$(readlink "$SOURCE")"
    [[ $SOURCE != /*  ]] && SOURCE="$DIR/$SOURCE"
done
DIR="$( cd -P "$( dirname "$SOURCE"  )" >/dev/null 2>&1 && pwd  )"

source "$DIR"/utils.sh

CEDANA_METRICS_ASR=${CEDANA_METRICS_ASR:-false}
CEDANA_METRICS_OTEL=${CEDANA_METRICS_OTEL:-false}
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
    if [ "$CEDANA_METRICS_OTEL" == "true" ]; then
        echo "Otel enabled..."
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
Environment=CEDANA_METRICS_ASR=$CEDANA_METRICS_ASR
Environment=CEDANA_METRICS_OTEL=$CEDANA_METRICS_OTEL
Environment=CEDANA_LOG_LEVEL=$CEDANA_LOG_LEVEL
Environment=CEDANA_URL=$CEDANA_URL
Environment=CEDANA_AUTH_TOKEN=$CEDANA_AUTH_TOKEN
Environment=CONTAINERS_HELPER_BINARY_DIR=/cedana/bin
Environment="PATH=/cedana/bin:${PATH}"
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
