#!/bin/bash
set -euo pipefail

# Remove config
rm -rf /host/root/.cedana/

# Remove temporary files and logs
rm -rf /host/var/log/*cedana*
rm -rf /host/tmp/*cedana*
rm -rf /host/run/*cedana*
rm -rf /host/dev/shm/*cedana*

# Remove all binaries and libraries from the host's filesystem
rm -f /host/usr/local/lib/libcedana*.so
rm -f /host/usr/local/bin/cedana
