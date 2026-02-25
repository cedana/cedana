#!/bin/bash
set -euo pipefail

# Load the binaries and libraries into the host's filesystem
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/lib/libcedana*.so /host/usr/local/lib/

# Reset config since it's a fresh install
rm -rf /host/root/.cedana/

echo "Installed Cedana into the host filesystem"
