#!/bin/bash

set -e

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
# NOTE: This script assumes it's executed in the container environment

mkdir -p /host/cedana /host/cedana/bin /host/cedana/scripts /host/cedana/lib

# We load the binary from docker image for the container
# Copy Cedana binaries and scripts to the host
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/lib/libcedana*.so /host/usr/local/lib/
cp /scripts/* /host/cedana/scripts
cp /Makefile /host/cedana/Makefile

cp /usr/local/bin/buildah /host/cedana/bin/buildah
cp /usr/local/bin/netavark /host/cedana/bin/netavark
cp /usr/local/bin/netavark-dhcp-proxy-client /host/cedana/bin/netavark-dhcp-proxy-client

# Enter chroot environment on the host
chroot /host /bin/bash /cedana/scripts/systemd-reset.sh
env \
    CEDANA_URL="$CEDANA_URL" \
    CEDANA_AUTH_TOKEN="$CEDANA_AUTH_TOKEN" \
    CEDANA_METRICS_ASR="$CEDANA_METRICS_ASR" \
    CEDANA_METRICS_OTEL="$CEDANA_METRICS_OTEL" \
    CEDANA_LOG_LEVEL="$CEDANA_LOG_LEVEL" \
    CONTAINERD_CONFIG_PATH="$CONTAINERD_CONFIG_PATH" \
    chroot /host /bin/bash /cedana/scripts/k8s-setup-host.sh
