#!/bin/bash

set -e

if [ -x /bin/systemctl ] || type systemctl > /dev/null 2>&1; then
    HAS_SYSTEMD=true
    return
fi

if [ "$HAS_SYSTEMD" == "true" ]; then
    chroot /host /bin/bash /cedana/scripts/host/systemd-install.sh
else
    chroot /host /usr/local/bin/cedana daemon start &> /var/log/cedana-daemon.log &
fi
