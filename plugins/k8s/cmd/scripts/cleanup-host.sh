#!/bin/bash

set -e

chroot /host make -C /cedana reset
chroot /host rm -rf /cedana
echo "Environment reset completed. Cedana has been uninstalled."
