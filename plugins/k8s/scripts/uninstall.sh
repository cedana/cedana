#!/bin/bash
set -euo pipefail

# Remove config
rm -rf /host/etc/cedana

# Remove temporary files and logs
rm -rf /host/var/log/*cedana*
rm -rf /host/tmp/*cedana*
rm -rf /host/run/*cedana*
rm -rf /host/dev/shm/*cedana*

# Remove all binaries and libraries from the host's filesystem
rm -f /host/$CEDANA_PLUGINS_LIB_DIR/*cedana*
rm -f /host/$CEDANA_PLUGINS_BIN_DIR/*cedana*
rm -f /host/$CEDANA_PLUGINS_BIN_DIR/criu # installed by the criu plugin

# Remove leftover systemd unit files (reset-service.sh already stops and
# disables the service when systemd is available)
rm -f /host/etc/systemd/system/cedana.service
rm -f /host/etc/systemd/system/multi-user.target.wants/cedana.service

# Remove the kubelet drop-in config added by configure-kubelet.sh
# (takes effect on the next kubelet restart)
find /host/etc /host/var/lib/kubelet -maxdepth 5 -type f -name "99-cedana.conf" -delete 2>/dev/null || true

############################################################
# Remove the cedana runtime from containerd configuration  #
############################################################

CONFIG_CHANGED=false

# Remove the cedana runtime block (3 lines) written by install-plugins.sh;
# returns non-zero if the file had no cedana runtime block
remove_cedana_runtime() {
    grep -qs 'runtimes."cedana"' "$1" || return 1
    echo "Removing cedana runtime config from $1"
    sed -i '/runtimes."cedana"/,+2d' "$1"
    CONFIG_CHANGED=true
}

CONTAINERD_CONFIG_PATH="/host${CONTAINERD_CONFIG_PATH:-/etc/containerd/config.toml}"
CONFD_FILE="$(dirname "$CONTAINERD_CONFIG_PATH")/conf.d/999-cedana.toml"

if [ -f "$CONFD_FILE" ]; then
    echo "Removing $CONFD_FILE"
    rm -f "$CONFD_FILE"
    CONFIG_CHANGED=true
fi
remove_cedana_runtime "$CONTAINERD_CONFIG_PATH" || true
# NOTE: the conf.d glob that install-plugins.sh may have added to the config's
# imports is left in place, as other conf.d files may rely on it by now.

# Resolve a k3s/RKE2 data dir (host path): --data-dir flag of the running
# process, then config.yaml, then the default location
rancher_data_dir() {
    local name="$1" pid args dir=""
    pid=$(pidof -s "$name" 2>/dev/null || true)
    if [ -n "$pid" ]; then
        args=$(ps -o args= -p "$pid" 2>/dev/null || true)
        dir=$(echo "$args" | sed -n 's/.*--data-dir[= ]\+\([^ ]*\).*/\1/p')
    fi
    if [ -z "$dir" ] && [ -f "/host/etc/rancher/$name/config.yaml" ]; then
        dir=$(sed -n 's/^[[:space:]]*data-dir:[[:space:]]*//p' "/host/etc/rancher/$name/config.yaml" | head -n 1 | tr -d '"' | tr -d "'")
    fi
    echo "${dir:-/var/lib/rancher/$name}"
}

# k3s/RKE2: remove the runtime from the containerd config templates, and remove
# a template entirely if we changed it and only the base include (written by
# us) remains
for RANCHER_NAME in rke2 k3s; do
    RANCHER_CONFIG_DIR="/host$(rancher_data_dir "$RANCHER_NAME")/agent/etc/containerd"
    for TEMPLATE in "$RANCHER_CONFIG_DIR/config.toml.tmpl" "$RANCHER_CONFIG_DIR/config-v3.toml.tmpl"; do
        remove_cedana_runtime "$TEMPLATE" || continue
        if [ -z "$(grep -v 'template "base"' "$TEMPLATE" | tr -d '[:space:]')" ]; then
            echo "Removing template $TEMPLATE (only the base include remains)"
            rm -f "$TEMPLATE"
        fi
    done
done

# Restart the container runtime so the cedana runtime is deregistered
# (k3s/RKE2 also re-render the containerd config from the remaining template)
if [ "$CONFIG_CHANGED" = true ]; then
    for SERVICE in containerd rke2-server rke2-agent k3s k3s-agent; do
        if chroot /host systemctl is-active --quiet "$SERVICE" 2>/dev/null; then
            echo "Restarting $SERVICE to remove the cedana runtime configuration..."
            chroot /host systemctl restart "$SERVICE" ||
                echo "WARNING: Failed to restart $SERVICE, please restart manually" >&2
            break
        fi
    done
fi
