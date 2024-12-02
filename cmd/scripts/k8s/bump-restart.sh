#!/bin/bash

# updates the cedana daemon to the latest version
# and restarts with the same arguments

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

mkdir -p /host/cedana /host/cedana/bin /host/cedana/scripts

# Copy Cedana binaries to the host
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/bin/build-start-daemon.sh /host/cedana/scripts/build-start-daemon.sh
cp /usr/local/bin/setup-host.sh /host/cedana/scripts/setup-host.sh

cp /usr/local/bin/buildah /host/cedana/bin/buildah
cp /usr/local/bin/netavark /host/cedana/bin/netavark
cp /usr/local/bin/netavark-dhcp-proxy-client /host/cedana/bin/netavark-dhcp-proxy-client

chroot /host /bin/bash /cedana/scripts/run-cedana.sh
