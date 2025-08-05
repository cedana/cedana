#!/bin/bash

set -eo pipefail

# Configure /dev/shm size
# This script increases the shared memory size

SHM_PATH="/dev/shm"
FSTAB="/etc/fstab"
SIZE=${SHM_CONFIG_SIZE:-"10G"}
MIN_SIZE=${SHM_CONFIG_MIN_SIZE:-"10G"}
MIN_BYTES=$(numfmt --from=iec "$MIN_SIZE")
SHM_CONFIG_ENABLED=${SHM_CONFIG_ENABLED:-"false"}

if [ "$SHM_CONFIG_ENABLED" != "true" ]; then
    echo "Shared memory configuration is not enabled, skipping..."
    exit 0
fi

echo "Configuring /dev/shm with size $SIZE..."

# 1. Remount if current size is too small
if [ "$(df --output=size -B 1 "$SHM_PATH" | tail -n 1)" -lt "$MIN_BYTES" ]; then
    echo "Remounting $SHM_PATH with size $SIZE..."
    mount -o remount,size="$SIZE" "$SHM_PATH"
else
    echo "$SHM_PATH already has sufficient size"
fi

# 2. Ensure fstab is correct for persistence
FSTAB_ENTRY="tmpfs /dev/shm tmpfs defaults,size=$SIZE 0 0"
if [ -f "$FSTAB" ] && grep -qE "^\s*[^#]\s*tmpfs\s+/dev/shm" "$FSTAB"; then
    echo "Updating existing fstab entry for /dev/shm..."
    sed -i.bak -E "s|^\s*[^#]\s*tmpfs\s+/dev/shm.*|$FSTAB_ENTRY|" "$FSTAB"
elif [ -f "$FSTAB" ]; then
    echo "Adding new fstab entry for /dev/shm..."
    echo "$FSTAB_ENTRY" >> "$FSTAB"
fi

echo "/dev/shm configuration complete."
