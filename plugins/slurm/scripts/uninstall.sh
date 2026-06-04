#!/bin/bash
set -euo pipefail

${CEDANA_PLUGINS_BIN_DIR}/cedana-slurm cleanup --node-role $CEDANA_SLURM_NODE_ROLE || true

# Remove config
rm -rf /etc/cedana

# Remove temporary files and logs
rm -rf /var/log/*cedana*
rm -rf /tmp/*cedana*
rm -rf /run/*cedana*
rm -rf /dev/shm/*cedana*

# Remove all binaries and libraries from the host's filesystem
rm -f /usr/local/lib/*cedana*
rm -f /usr/local/bin/*cedana*
