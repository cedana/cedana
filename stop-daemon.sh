#!/bin/bash

set -e

SUDO_USE=sudo
if ! which sudo &>/dev/null; then
    SUDO_USE=""
fi

APP_NAME="cedana"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"

echo "Stopping $APP_NAME service..."
$SUDO_USE systemctl stop $APP_NAME.service

# truncate the logs
echo -n > /var/log/cedana-daemon.log
