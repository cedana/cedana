#!/bin/bash

set -eo pipefail

chroot /host sh -c '
if [ -d /cedana ]; then
    make -C /cedana reset
    rm -rf /cedana
    echo "Environment reset completed. Cedana has been uninstalled."
else
    echo "/cedana does not exist. Nothing to reset."
fi
'
