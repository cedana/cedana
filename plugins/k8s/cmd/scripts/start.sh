#!/bin/bash

set -eo pipefail

if [ -f /host/.dockerenv ]; then # for tests
    chroot /host /usr/local/bin/cedana daemon start &> /var/log/cedana-daemon.log &
else
    chroot /host /bin/bash /cedana/scripts/host/systemd-install.sh
fi
