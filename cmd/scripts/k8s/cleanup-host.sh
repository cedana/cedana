#!/bin/bash

chroot /host /bin/bash <<"EOT"
rm -rf /cedana

rm -rf /criu

rm -rf /tmp/cedana*
rm -rf /tmp/sqlite_cedana*
rm -rf /var/log/cedana*
rm -rf /dev/shm/cedana*

pkill cdp
pkill otelcol-contrib

systemctl stop cedana.service


rm -f /usr/local/bin/cedana
rm -f /build-start-daemon.sh
EOT

echo "Clean up completed."
