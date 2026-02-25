#!/bin/bash
set -euo pipefail

# NOTE: This is called from a Cedana binary so assuming it's already installed

# Reset config since it's a fresh install
rm -rf ~/.cedana/

echo "Installed Cedana into the host filesystem"
