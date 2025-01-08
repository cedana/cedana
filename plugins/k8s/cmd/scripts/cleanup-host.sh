#!/bin/bash

set -e

chroot /host make -C /cedana reset
echo "Environment reset completed. Cedana has been uninstalled."
