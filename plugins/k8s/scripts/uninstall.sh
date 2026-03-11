#!/bin/bash
set -euo pipefail

check_root

# Remove config
rm -rf /host/etc/cedana

# Remove temporary files and logs
rm -rf /host/var/log/*cedana*
rm -rf /host/tmp/*cedana*
rm -rf /host/run/*cedana*
rm -rf /host/dev/shm/*cedana*

# Remove all binaries and libraries from the host's filesystem
rm -f /host/usr/local/lib/*cedana*
rm -f /host/usr/local/bin/*cedana*
