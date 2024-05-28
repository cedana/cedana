#!/bin/bash

# Define variables
APP_NAME="cedana"
APP_PATH="/usr/local/bin/$APP_NAME"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"
USER=$(whoami)
CEDANA_GPU_ENABLED=${CEDANA_GPU_ENABLED:-0}
CEDANA_OTEL_ENABLED=${CEDANA_OTEL_ENABLED:-0}
CEDANA_GPU_CONTROLLER_PATH="/usr/local/bin/gpu-controller"
CEDANA_PROFILING_ENABLED=${CEDANA_PROFILING_ENABLED:-0}
CEDANA_IS_K8S=${CEDANA_IS_K8S:-0}
CEDANA_GPU_DEBUGGING_ENABLED=${CEDANA_GPU_DEBUGGING_ENABLED:-0}
USE_SYSTEMCTL=0

# Check for --systemctl flag
for arg in "$@"; do
    if [ "$arg" == "--systemctl" ]; then
        USE_SYSTEMCTL=1
    fi
done

export PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION="python"

echo "Building $APP_NAME..."
go build

if [ $? -ne 0 ]; then
    echo "Build failed. Exiting."
    exit 1
fi

sudo cp $APP_NAME $APP_PATH

# if cedana_gpu_enabled=atrue, echo
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
    cat <<EOF | sudo tee $SERVICE_FILE >/dev/null
[Unit]
Description=Cedana Checkpointing Daemon
[Service]
Environment=USER=$USER
Environment=CEDANA_GPU_ENABLED=$CEDANA_GPU_ENABLED
Environment=CEDANA_GPU_CONTROLLER_PATH=$CEDANA_GPU_CONTROLLER_PATH
Environment=CEDANA_PROFILING_ENABLED=$CEDANA_PROFILING_ENABLED
Environment=CEDANA_OTEL_ENABLED=$CEDANA_OTEL_ENABLED
Environment=CEDANA_IS_K8S=$CEDANA_IS_K8S
Environment=CEDANA_GPU_DEBUGGING_ENABLED=$CEDANA_GPU_DEBUGGING_ENABLED
ExecStart=$APP_PATH daemon start
User=root
Group=root
Restart=no

[Install]
WantedBy=multi-user.target

[Service]
StandardError=append:/var/log/cedana-daemon.log
EOF

    echo "Reloading systemd..."
    sudo systemctl daemon-reload

    echo "Enabling and starting $APP_NAME service..."
    sudo systemctl enable $APP_NAME.service
    sudo systemctl start $APP_NAME.service
    echo "$APP_NAME service setup complete."
else
    echo "Starting daemon as a background process..."
    sudo $APP_PATH daemon start &
    echo "$APP_NAME daemon started as a background process."
fi
