#!/bin/bash

# Define variables
APP_NAME="cedana"
APP_PATH="/usr/local/bin/$APP_NAME"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"
USER=$(whoami)

# Step 1: Build your Go program
echo "Building $APP_NAME..."
go build 

sudo cp $APP_NAME $APP_PATH

# Check if build was successful
if [ $? -ne 0 ]; then
    echo "Build failed. Exiting."
    exit 1
fi

# Step 2: Create systemd service file
echo "Creating $SERVICE_FILE..."
cat <<EOF | sudo tee $SERVICE_FILE > /dev/null
[Unit]
Description=Cedana Checkpointing Daemon
[Service]
Environment=USER=$USER
ExecStart=$APP_PATH daemon start 
User=root
Group=root
Restart=no

[Install]
WantedBy=multi-user.target
EOF

# Step 3: Reload systemd to recognize the new service
echo "Reloading systemd..."
sudo systemctl daemon-reload

# Step 4: Enable and start the service
echo "Enabling and starting $APP_NAME service..."
sudo systemctl enable $APP_NAME.service
sudo systemctl start $APP_NAME.service

echo "$APP_NAME service setup complete."
