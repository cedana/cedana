#!/bin/bash

cp /usr/local/bin/stop-daemon.sh /host/stop-daemon.sh

chroot /host <<"EOT"
cd /
chmod +x stop-daemon.sh
./stop-daemon.sh --systemctl
EOT

# update Cedana binary
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/bin/build-start-daemon.sh /host/build-start-daemon.sh

chroot /host <<"EOT"
cd /
./build-start-daemon.sh --systemctl --no-build --k8s
EOT
