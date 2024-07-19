#!/bin/bash

set -e

SUDO_USE=sudo
if ! which sudo &>/dev/null; then
    SUDO_USE=""
fi

# Define variables
APP_NAME="cedana"
APP_PATH="/usr/local/bin/$APP_NAME"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"
USER=$(whoami)
CEDANA_OTEL_ENABLED=${CEDANA_OTEL_ENABLED:-0}
CEDANA_GPU_CONTROLLER_PATH="/usr/local/bin/cedana-gpu-controller"
CEDANA_PROFILING_ENABLED=${CEDANA_PROFILING_ENABLED:-0}
CEDANA_IS_K8S=${CEDANA_IS_K8S:-0}
CEDANA_GPU_ENABLED=false
CEDANA_GPU_DEBUGGING_ENABLED=${CEDANA_GPU_DEBUGGING_ENABLED:-0}
USE_SYSTEMCTL=0
NO_BUILD=0

# Check for --systemctl flag
for arg in "$@"; do
    if [ "$arg" == "--systemctl" ]; then
        echo "Using systemctl"
        USE_SYSTEMCTL=1
    fi
done

# Check for --no-build flag
for arg in "$@"; do
    if [ "$arg" == "--no-build" ]; then
        echo "Skipping build"
        NO_BUILD=1
    fi
done


# Check for --gpu flag
for arg in "$@"; do
    if [ "$arg" == "--gpu" ]; then
        echo "GPU support enabled"
        CEDANA_GPU_ENABLED=true
    fi
done

export PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION="python"

if [ $NO_BUILD -ne 1 ]; then
    echo "Building $APP_NAME..."
    go build

    if [ $? -ne 0 ]; then
        echo "Build failed. Exiting."
        exit 1
    else
        $SUDO_USE cp $APP_NAME $APP_PATH
    fi
else
    echo "Skipping build..."
    $SUDO_USE cp $APP_NAME $APP_PATH
fi

if [ "$CEDANA_GPU_ENABLED" = "true" ]; then
    echo "Starting daemon with GPU support..."
fi

if [ "$CEDANA_OTEL_ENABLED" = "true" ]; then
    echo "Starting daemon with OpenTelemetry support..."
fi

if [ "$CEDANA_GPU_DEBUGGING_ENABLED" = "true" ]; then
    echo "Starting daemon with GPU debugging support..."
fi

if [ $USE_SYSTEMCTL -eq 1 ]; then
    # create systemd file
    echo "Creating $SERVICE_FILE..."
    cat <<EOF | $SUDO_USE tee $SERVICE_FILE >/dev/null
[Unit]
Description=Cedana Checkpointing Daemon
[Service]
Environment=USER=$USER
Environment=CEDANA_GPU_CONTROLLER_PATH=$CEDANA_GPU_CONTROLLER_PATH
Environment=CEDANA_PROFILING_ENABLED=$CEDANA_PROFILING_ENABLED
Environment=CEDANA_OTEL_ENABLED=$CEDANA_OTEL_ENABLED
Environment=CEDANA_IS_K8S=$CEDANA_IS_K8S
Environment=CEDANA_GPU_DEBUGGING_ENABLED=$CEDANA_GPU_DEBUGGING_ENABLED
ExecStart=$APP_PATH daemon start --gpu-enabled=$CEDANA_GPU_ENABLED
User=root
Group=root
Restart=no

[Install]
WantedBy=multi-user.target

[Service]
StandardError=append:/var/log/cedana-daemon.log
EOF

    echo "Reloading systemd..."
    $SUDO_USE systemctl daemon-reload

    echo "Enabling and starting $APP_NAME service..."
    $SUDO_USE systemctl enable $APP_NAME.service
    $SUDO_USE systemctl start $APP_NAME.service
    echo "$APP_NAME service setup complete."
else
    echo "Starting daemon as a background process..."
    if [[ ! -n "${SUDO_USE}" ]]; then
        $APP_PATH daemon start --gpu-enabled="$CEDANA_GPU_ENABLED" &
    else
        $SUDO_USE -E $APP_PATH daemon start --gpu-enabled="$CEDANA_GPU_ENABLED" &
    fi
    echo "$APP_NAME daemon started as a background process."
fi
