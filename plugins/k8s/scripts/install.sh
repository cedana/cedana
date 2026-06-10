#!/bin/bash
set -euo pipefail

# Load the binaries and libraries into the host's filesystem
cp $APP_PATH /host/$APP_PATH
cp $CEDANA_PLUGINS_LIB_DIR/libcedana*.so /host/$CEDANA_PLUGINS_LIB_DIR/
cp $CEDANA_PLUGINS_BIN_DIR/*cedana* /host/$CEDANA_PLUGINS_BIN_DIR/

# Re-initialize config since it's a fresh install
chroot /host $APP_PATH --merge-config version
