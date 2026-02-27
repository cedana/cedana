#!/bin/bash
set -euo pipefail

cedana-slurm cleanup || true

# Remove config
rm -rf /etc/cedana

# Remove temporary files and logs
rm -rf /var/log/*cedana*
rm -rf /tmp/*cedana*
rm -rf /run/*cedana*
rm -rf /dev/shm/*cedana*

# Remove all binaries and libraries from the host's filesystem
rm -f /usr/local/lib/libcedana*.so
rm -f /usr/local/bin/cedana
