#!/bin/bash
set -euo pipefail

# Load the binaries and libraries into the host's filesystem
cp $APP_PATH /host/$APP_PATH
cp $CEDANA_PLUGINS_LIB_DIR/libcedana*.so /host/$CEDANA_PLUGINS_LIB_DIR/
cp $CEDANA_PLUGINS_BIN_DIR/*cedana* /host/$CEDANA_PLUGINS_BIN_DIR/

# Re-initialize config since it's a fresh install
chroot /host $APP_PATH --merge-config version

# Configure dynamic linker for NVIDIA libraries on the GPU K8 hosts if not configured already
nvidia_lib_path="/run/nvidia/driver/usr/lib/x86_64-linux-gnu"
if [ -d "/host$nvidia_lib_path" ]; then
    echo "NVIDIA drivers detected, checking ldconfig configuration!"
    if [ ! -f "/host/etc/ld.so.conf.d/nvidia.conf" ] || ! grep -qxF "$nvidia_lib_path" "/host/etc/ld.so.conf.d/nvidia.conf" 2>/dev/null; then
        echo "Adding ldconfig path: $nvidia_lib_path"
        echo "$nvidia_lib_path" >>/host/etc/ld.so.conf.d/nvidia.conf
        if ! chroot /host ldconfig; then
            echo "Failed to update the host ldconfig cache."
        fi
        echo "ldconfig has been set successfully!"
    else
        echo "NVIDIA ldconfig path has already been configured."
    fi
fi
