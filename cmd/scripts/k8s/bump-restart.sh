#!/bin/bash

cp /usr/local/bin/stop-daemon.sh /host/stop-daemon.sh

chroot /host <<"EOT"
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

# delete old logs
rm -rf /var/log/cedana*
EOT

# update Cedana binary
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/bin/build-start-daemon.sh /host/build-start-daemon.sh

chroot /host <<"EOT"
cd /
IS_K8S=1 ./build-start-daemon.sh --systemctl --no-build
EOT
