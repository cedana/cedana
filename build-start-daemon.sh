#!/bin/bash

# Define variables
APP_NAME="cedana"
APP_PATH="/usr/local/bin/$APP_NAME"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"
USER=$(whoami)
CEDANA_GPU_ENABLED=${CEDANA_GPU_ENABLED:-0}
GPU_CONTROLLER_PATH="/usr/local/bin/cedana-gpu-controller"
CEDANA_PROFILING_ENABLED=${CEDANA_PROFILING_ENABLED:-0}

echo "Building $APP_NAME..."
go build 

sudo cp $APP_NAME $APP_PATH

if [ $? -ne 0 ]; then
    echo "Build failed. Exiting."
    exit 1
fi

# if cedana_gpu_enabled=atrue, echo 
if [ "$CEDANA_GPU_ENABLED" = "true" ]; then
    echo "Starting daemon with GPU support..."
fi

# create systemd file 
echo "Creating $SERVICE_FILE..."
cat <<EOF | sudo tee $SERVICE_FILE > /dev/null
[Unit]
Description=Cedana Checkpointing Daemon
[Service]
Environment=USER=$USER
Environment=CEDANA_GPU_ENABLED=$CEDANA_GPU_ENABLED
Environment=GPU_CONTROLLER_PATH=$GPU_CONTROLLER_PATH
Environment=CEDANA_PROFILING_ENABLED=$CEDANA_PROFILING_ENABLED
ExecStart=$APP_PATH daemon start 
User=root
Group=root
Restart=no

[Install]
WantedBy=multi-user.target
EOF

echo "Reloading systemd..."
sudo systemctl daemon-reload

echo "Enabling and starting $APP_NAME service..."
sudo systemctl enable $APP_NAME.service
sudo systemctl start $APP_NAME.service

echo "$APP_NAME service setup complete."
