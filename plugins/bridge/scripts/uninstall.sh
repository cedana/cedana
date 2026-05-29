#!/bin/bash
set -euo pipefail

check_root

SERVICE_NAME="cedana-bridge"

systemctl stop "${SERVICE_NAME}.service" || true
systemctl disable "${SERVICE_NAME}.service" || true
rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
systemctl daemon-reload || true

rm -f /var/run/cedana-current-jid
rm -f /var/run/cedana-last-action-id
rm -f /var/run/cedana-last-checkpoint

echo "Bridge plugin cleanup complete"
