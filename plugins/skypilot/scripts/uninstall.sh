#!/bin/bash
set -euo pipefail

# SkyPilot plugin cleanup script
# Removes SkyPilot-specific state files

rm -f /var/run/cedana-current-jid
rm -f /var/run/cedana-last-action-id
rm -f /var/run/cedana-last-checkpoint

echo "SkyPilot plugin cleanup complete"
