#!/bin/bash

chroot /host make -C /cedana reset
echo "Environment reset completed. Cedana has been uninstalled."
