#!/bin/bash

set -eo pipefail

if [ -f /host/.dockerenv ]; then
    chroot /host pkill -f 'cedana daemon' || true
else
    chroot /host /bin/bash /cedana/scripts/host/systemd-reset.sh
fi
