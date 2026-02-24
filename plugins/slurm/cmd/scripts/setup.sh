#!/bin/bash

set -eo pipefail

if [ -f /host/.dockerenv ]; then # for tests
    pkill -f 'cedana daemon' || true
else
    /cedana/scripts/host/systemd-setup.sh
fi
