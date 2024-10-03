#!/bin/bash

chroot /host /bin/bash <<'EOT'
APP_NAME="cedana"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"

echo "Stopping $APP_NAME service..."
$SUDO_USE systemctl stop $APP_NAME.service

# truncate the logs
echo -n > /var/log/cedana-daemon.log
EOT

# Copy Cedana binaries to the host
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/bin/build-start-daemon.sh /host/build-start-daemon.sh
cp /usr/local/bin/setup-host.sh /host/setup-host.sh

# Enter chroot environment on the host
env \
    CEDANA_URL="$CEDANA_URL" \
    CEDANA_AUTH_TOKEN="$CEDANA_AUTH_TOKEN" \
    CEDANA_OTEL_ENABLED="$CEDANA_OTEL_ENABLED" \
    CEDANA_PORT="$CEDANA_PORT" \
    CEDANA_OTEL_PORT="$CEDANA_OTEL_PORT" \
    CEDANA_LOG_LEVEL="$CEDANA_LOG_LEVEL" \
    SKIPSETUP="$CEDANA_SKIPSETUP" \
    chroot /host /bin/bash ./setup-host.sh
