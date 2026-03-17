#!/bin/bash
set -euo pipefail

# Bridge plugin cleanup script
# Removes Bridge-specific state files

rm -f /var/run/cedana-current-jid
rm -f /var/run/cedana-last-action-id
rm -f /var/run/cedana-last-checkpoint

echo "Bridge plugin cleanup complete"
