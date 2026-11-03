#!/bin/bash
set -euo pipefail

# Raises fs.inotify.max_user_instances when it's low: CRIU needs inotify instances to dump and
# restore, and nodes often have the kernel default of 128, which busy nodes run out of. Never
# lowers a value that's already higher. Persisted in sysctl.d so it survives a node reboot.

MIN_INSTANCES=${CEDANA_INOTIFY_MAX_USER_INSTANCES:-8192}
PROC_PATH=${INOTIFY_MAX_USER_INSTANCES_PATH:-/proc/sys/fs/inotify/max_user_instances}
SYSCTL_CONF=${SYSCTL_CONF_PATH:-/etc/sysctl.d/99-cedana.conf}
KEY="fs.inotify.max_user_instances"

if [ ! -r "$PROC_PATH" ]; then
    echo "WARNING: $PROC_PATH not found, can't check $KEY. CRIU needs inotify: if it's low, dumps and restores can fail" >&2
    exit 0
fi

current=$(cat "$PROC_PATH")
echo "$KEY is $current (want at least $MIN_INSTANCES)"

if [ "$current" -lt "$MIN_INSTANCES" ]; then
    if echo "$MIN_INSTANCES" >"$PROC_PATH" 2>/dev/null; then
        echo "Raised $KEY from $current to $MIN_INSTANCES"
        current=$MIN_INSTANCES
    else
        echo "WARNING: failed to raise $KEY from $current to $MIN_INSTANCES. CRIU needs inotify: dumps and restores can fail. Raise it with: sysctl -w $KEY=$MIN_INSTANCES" >&2
        exit 0
    fi
fi

# Persist what's in effect (at least MIN_INSTANCES), so a reboot doesn't take it back to the default
mkdir -p "$(dirname "$SYSCTL_CONF")"
if [ -f "$SYSCTL_CONF" ] && grep -qE "^\s*$KEY\s*=" "$SYSCTL_CONF"; then
    sed -i -E "s|^\s*$KEY\s*=.*|$KEY = $current|" "$SYSCTL_CONF"
else
    echo "$KEY = $current" >>"$SYSCTL_CONF"
fi
echo "Persisted $KEY = $current in $SYSCTL_CONF"
