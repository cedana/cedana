#!/bin/bash
set -euo pipefail

check_root

cedana-slurm cleanup || true

# Remove config
rm -rf /etc/cedana

# Remove temporary files and logs
rm -rf /var/log/*cedana*
rm -rf /tmp/*cedana*
rm -rf /run/*cedana*
rm -rf /dev/shm/*cedana*

# Remove all binaries and libraries from the host's filesystem
rm -f /host/usr/local/lib/*cedana*
rm -f /host/usr/local/bin/*cedana*
