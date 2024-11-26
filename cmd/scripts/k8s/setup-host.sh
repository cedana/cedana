#!/bin/bash

chroot /host /bin/bash <<'EOT'
APP_NAME="cedana"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"

if [ -f /var/log/cedana-daemon.log ]; then
    echo "Stopping $APP_NAME service..."
    $SUDO_USE systemctl stop $APP_NAME.service

    # truncate the logs
    echo -n > /var/log/cedana-daemon.log
else
    echo "No cedana-daemon.log file found"
    echo "Skipping $APP_NAME service stop"
fi
EOT

# Create Cedana directories
mkdir -p /host/cedana /host/cedana/bin /host/cedana/scripts

# We load the binary from docker image for the container
# Copy Cedana binaries to the host
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/bin/build-start-daemon.sh /host/cedana/scripts/build-start-daemon.sh
cp /usr/local/bin/setup-host.sh /host/cedana/scripts/setup-host.sh
cp /usr/local/bin/reset.sh /host/cedana/scripts/reset.sh


cp /usr/local/bin/buildah /host/cedana/bin/buildah
cp /usr/local/bin/netavark /host/cedana/bin/netavark
cp /usr/local/bin/netavark-dhcp-proxy-client /host/cedana/bin/netavark-dhcp-proxy-client


# Enter chroot environment on the host
env \
    CEDANA_URL="$CEDANA_URL" \
    CEDANA_AUTH_TOKEN="$CEDANA_AUTH_TOKEN" \
    CEDANA_OTEL_ENABLED="$CEDANA_OTEL_ENABLED" \
    CEDANA_OTEL_PORT="$CEDANA_OTEL_PORT" \
    CEDANA_LOG_LEVEL="$CEDANA_LOG_LEVEL" \
    SKIPSETUP="$CEDANA_SKIPSETUP" \
    chroot /host /bin/bash /cedana/scripts/setup-host.sh
