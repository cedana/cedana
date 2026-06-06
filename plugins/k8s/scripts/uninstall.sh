#!/bin/bash
set -euo pipefail

# Remove config
rm -rf /host/etc/cedana

# Remove temporary files and logs
rm -rf /host/var/log/*cedana*
rm -rf /host/tmp/*cedana*
rm -rf /host/run/*cedana*
rm -rf /host/dev/shm/*cedana*

# Remove all binaries and libraries from the host's filesystem
rm -f /host/$CEDANA_PLUGINS_LIB_DIR/*cedana*
rm -f /host/$CEDANA_PLUGINS_BIN_DIR/*cedana*
