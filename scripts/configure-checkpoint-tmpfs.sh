#!/bin/bash
set -euo pipefail

# Source utils.sh if running as a standalone script (BASH_SOURCE is set)
if [ -n "${BASH_SOURCE[0]:-}" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ -f "$SCRIPT_DIR/utils.sh" ]; then
        source "$SCRIPT_DIR/utils.sh"
    fi
fi

# Provides /checkpoint as tmpfs, the memory storage tier for Cedana checkpoints.
#
# tmpfs because a checkpoint's value is speed: restoring a model from RAM avoids
# a weight load that measures eleven minutes from block storage on this hardware.
# The trade is that the checkpoint dies with the node, so this tier serves model
# swaps on a live node and cannot serve recovery from node loss — that needs a
# durable tier.
#
# tmpfs pages are charged to node memory, and they compete directly with the
# workload: a GPU worker here requests 48Gi and may limit at 80Gi on a node with
# ~85GiB. An oversized /checkpoint does not fail loudly, it makes the kernel
# reclaim or OOM-kill under load, so the size ceiling below is a safety property
# rather than a preference.

CHECKPOINT_PATH=${CHECKPOINT_TMPFS_PATH:-"/checkpoint"}
FSTAB="/etc/fstab"
SIZE=${CHECKPOINT_TMPFS_SIZE:-"50G"}
MODE=${CHECKPOINT_TMPFS_MODE:-"1777"}
ENABLED=${CHECKPOINT_TMPFS_ENABLED:-"false"}
# Share of host RAM this mount may claim. tmpfs is allowed to grow to its size,
# so a value near or above total RAM is a latent node failure.
MAX_RAM_FRACTION=${CHECKPOINT_TMPFS_MAX_RAM_FRACTION:-80}

if [ "$ENABLED" != "true" ]; then
    echo "Checkpoint tmpfs is not enabled, skipping..."
    exit 0
fi

SIZE_BYTES=$(numfmt --from=iec "$SIZE")
TOTAL_RAM_BYTES=$(($(awk '/^MemTotal:/ {print $2}' /proc/meminfo) * 1024))
MAX_ALLOWED_BYTES=$((TOTAL_RAM_BYTES * MAX_RAM_FRACTION / 100))

echo "Configuring $CHECKPOINT_PATH as tmpfs, size $SIZE, mode $MODE"
echo "Host RAM: $(numfmt --to=iec "$TOTAL_RAM_BYTES"), ceiling ${MAX_RAM_FRACTION}%: $(numfmt --to=iec "$MAX_ALLOWED_BYTES")"

# Refuse rather than clamp. Silently shrinking the mount would let a checkpoint
# fail mid-write with no space, which is harder to diagnose than not starting.
if [ "$SIZE_BYTES" -gt "$MAX_ALLOWED_BYTES" ]; then
    echo "ERROR: requested size $SIZE exceeds ${MAX_RAM_FRACTION}% of host RAM ($(numfmt --to=iec "$MAX_ALLOWED_BYTES"))." >&2
    echo "       tmpfs is charged to node memory and competes with the workload." >&2
    exit 1
fi

mkdir -p "$CHECKPOINT_PATH"

# Idempotent: mounted at the right size and mode is success, not a reason to
# remount. The helper runs this on every start.
needs_mount=1
if mountpoint -q "$CHECKPOINT_PATH"; then
    CURRENT_FSTYPE=$(findmnt -no FSTYPE "$CHECKPOINT_PATH")
    CURRENT_BYTES=$(df -B1 --output=size "$CHECKPOINT_PATH" | awk 'NR==2 {print $1}')
    if [ "$CURRENT_FSTYPE" != "tmpfs" ]; then
        echo "ERROR: $CHECKPOINT_PATH is mounted as $CURRENT_FSTYPE, not tmpfs." >&2
        echo "       Refusing to replace a non-tmpfs mount that may hold data." >&2
        exit 1
    fi
    if [ "$CURRENT_BYTES" -eq "$SIZE_BYTES" ]; then
        echo "$CHECKPOINT_PATH already tmpfs at the requested size"
        needs_mount=0
    else
        echo "Remounting: size is $(numfmt --to=iec "$CURRENT_BYTES"), want $SIZE"
        mount -o "remount,size=$SIZE,mode=$MODE" "$CHECKPOINT_PATH"
        needs_mount=0
    fi
fi

if [ "$needs_mount" -eq 1 ]; then
    echo "Mounting tmpfs at $CHECKPOINT_PATH"
    if ! mount -t tmpfs -o "size=$SIZE,mode=$MODE" tmpfs "$CHECKPOINT_PATH"; then
        echo "ERROR: failed to mount tmpfs at $CHECKPOINT_PATH" >&2
        exit 1
    fi
fi

# Mode is set explicitly: a checkpoint is written by the shim and may be read by
# a differently-owned restore process, and mount options do not always apply the
# mode to an existing directory.
chmod "$MODE" "$CHECKPOINT_PATH"

# Persist so a node reboot does not silently lose the tier. The checkpoint
# contents do not survive either way — this only keeps the mount present, so a
# restore attempt fails a compatibility check rather than writing to the root
# filesystem and filling it.
FSTAB_ENTRY="tmpfs $CHECKPOINT_PATH tmpfs defaults,size=$SIZE,mode=$MODE 0 0"
if [ -f "$FSTAB" ] && grep -qE "^\s*[^#]*\s+${CHECKPOINT_PATH}\s+tmpfs" "$FSTAB"; then
    if ! grep -qF "$FSTAB_ENTRY" "$FSTAB"; then
        echo "Updating existing $FSTAB entry for $CHECKPOINT_PATH"
        sed -i "\|^\s*[^#]*\s\+${CHECKPOINT_PATH}\s\+tmpfs.*|c\\${FSTAB_ENTRY}" "$FSTAB"
    fi
else
    echo "Adding $FSTAB entry for $CHECKPOINT_PATH"
    echo "$FSTAB_ENTRY" >>"$FSTAB"
fi

# Report what the tier can actually hold. A checkpoint of a 27B FP8 model is
# ~30GB, so an operator sizing this needs the number, not just success.
echo "Checkpoint tmpfs ready:"
df -h "$CHECKPOINT_PATH" | awk 'NR==2 {print "  " $2 " total, " $4 " available at " $6}'
