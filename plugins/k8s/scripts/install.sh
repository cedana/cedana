#!/bin/bash
set -euo pipefail

# Load the binaries and libraries into the host's filesystem
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/lib/libcedana*.so /host/usr/local/lib/

# Re-initialize config since it's a fresh install
chroot /host /usr/local/bin/cedana --init-config version

echo "Installed Cedana into the host filesystem"
