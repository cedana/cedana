#!/bin/bash
set -euo pipefail

check_root

# NOTE: This is called from a Cedana binary so assuming it's already installed

# Re-initialize config since it's a fresh install
/usr/local/bin/cedana --init-config version

echo "Installed Cedana SkyPilot plugin into the host filesystem"
