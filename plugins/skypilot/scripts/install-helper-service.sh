#!/bin/bash
set -euo pipefail

check_root

SERVICE_NAME="cedana-skypilot-helper"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

echo "Creating $SERVICE_FILE..."
cat <<EOF | tee "$SERVICE_FILE" >/dev/null
[Unit]
Description=Cedana SkyPilot Helper
After=cedana.service
Requires=cedana.service

[Service]
Type=simple
ExecStart=/usr/local/bin/cedana skypilot start
User=root
Group=root
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

echo "Reloading systemd..."
systemctl daemon-reload

echo "Enabling and starting $SERVICE_NAME service..."
systemctl enable "$SERVICE_NAME".service
systemctl start "$SERVICE_NAME".service
echo "$SERVICE_NAME service setup complete."
