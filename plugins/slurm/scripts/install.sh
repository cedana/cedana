#!/bin/bash
set -euo pipefail

# NOTE: This is called from a Cedana binary so assuming it's already installed

# Merge new config defaults without overwriting existing user config values
$APP_PATH --merge-config version

echo "Installed Cedana into the host filesystem"
