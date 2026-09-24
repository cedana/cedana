#!/bin/bash
set -euo pipefail

CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-"release"}
CEDANA_PLUGINS_NATIVE_VERSION=${CEDANA_PLUGINS_NATIVE_VERSION:-"latest"}
CEDANA_PLUGINS_CRIU_VERSION=${CEDANA_PLUGINS_CRIU_VERSION:-"latest"}
CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION=${CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION:-"latest"}
CEDANA_PLUGINS_GPU_VERSION=${CEDANA_PLUGINS_GPU_VERSION:-"latest"}
CEDANA_PLUGINS_STREAMER_VERSION=${CEDANA_PLUGINS_STREAMER_VERSION:-"latest"}
CEDANA_CHECKPOINT_DIR=${CEDANA_CHECKPOINT_DIR:-"/tmp"}
CEDANA_CHECKPOINT_STREAMS=${CEDANA_CHECKPOINT_STREAMS:-0}

echo "Starting cedana plugin setup"
echo "Config: BUILDS=$CEDANA_PLUGINS_BUILDS NATIVE=$CEDANA_PLUGINS_NATIVE_VERSION CRIU=$CEDANA_PLUGINS_CRIU_VERSION CONTAINERD_RUNTIME=$CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION GPU=$CEDANA_PLUGINS_GPU_VERSION STREAMER=$CEDANA_PLUGINS_STREAMER_VERSION"
echo "Config: CHECKPOINT_DIR=$CEDANA_CHECKPOINT_DIR CHECKPOINT_STREAMS=$CEDANA_CHECKPOINT_STREAMS"

# XXX: We always install the GPU plugin because the race w/ gpu-operator (if the cluster is using it)
# is not worth defending against. In any case, the resources check on using gpus in the yaml will prevent
# a GPU pod from being scheduled.
PLUGINS=" \
    criu@$CEDANA_PLUGINS_CRIU_VERSION \
    containerd/runtime-runc@$CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION \
    containerd@$CEDANA_PLUGINS_NATIVE_VERSION \
    runc@$CEDANA_PLUGINS_NATIVE_VERSION"

PLUGINS_TO_REMOVE=""

if [ "$CEDANA_PLUGINS_GPU_VERSION" != "none" ]; then
    echo "GPU plugin enabled (version=$CEDANA_PLUGINS_GPU_VERSION), adding to install list"
    PLUGINS="$PLUGINS gpu@$CEDANA_PLUGINS_GPU_VERSION"
else
    echo "GPU plugin disabled (CEDANA_PLUGINS_GPU_VERSION=none), marking for removal"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE gpu"
fi

# check if a storage plugin is required
echo "Determining storage plugin from CEDANA_CHECKPOINT_DIR=$CEDANA_CHECKPOINT_DIR..."
if [[ "$CEDANA_CHECKPOINT_DIR" == cedana://* ]]; then
    echo "Cedana storage backend detected, adding storage/cedana plugin"
    PLUGINS="$PLUGINS storage/cedana@$CEDANA_PLUGINS_NATIVE_VERSION"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/s3 storage/gcs storage/csx"
elif [[ "$CEDANA_CHECKPOINT_DIR" == s3://* ]]; then
    echo "S3 storage backend detected, adding storage/s3 plugin"
    PLUGINS="$PLUGINS storage/s3@$CEDANA_PLUGINS_NATIVE_VERSION"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/cedana storage/gcs storage/csx"
elif [[ "$CEDANA_CHECKPOINT_DIR" == gcs://* ]]; then
    echo "GCS storage backend detected, adding storage/gcs plugin"
    PLUGINS="$PLUGINS storage/gcs@$CEDANA_PLUGINS_NATIVE_VERSION"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/cedana storage/s3 storage/csx"
elif [[ "$CEDANA_CHECKPOINT_DIR" == csx://* ]]; then
    echo "CSX storage backend detected, adding storage/csx plugin"
    PLUGINS="$PLUGINS storage/csx@$CEDANA_PLUGINS_NATIVE_VERSION"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/cedana storage/s3 storage/gcs"
else
    echo "Local storage backend detected, no storage plugin needed"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/cedana storage/s3 storage/gcs storage/csx"
fi

# check if streamer plugin is required
echo "Checking streamer plugin (CEDANA_CHECKPOINT_STREAMS=$CEDANA_CHECKPOINT_STREAMS)..."
if [ "$CEDANA_CHECKPOINT_STREAMS" -gt 0 ]; then
    echo "Streaming enabled, adding streamer@$CEDANA_PLUGINS_STREAMER_VERSION plugin"
    PLUGINS="$PLUGINS streamer@$CEDANA_PLUGINS_STREAMER_VERSION"
else
    echo "Streaming disabled, marking streamer for removal"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE streamer"
fi

echo "Plugins to install:$PLUGINS"
echo "Plugins to remove: $PLUGINS_TO_REMOVE"

# If gpu driver present then add gpu plugin
# NOTE: This is no longer used to conditionally add the gpu plugin, but we still
# log the driver version here for informational purposes.
echo "Checking for NVIDIA GPU presence..."
if [ -d /proc/driver/nvidia/gpus/ ]; then
    if [ ! -d /run/driver/nvidia ]; then
        # Check if the NVIDIA driver is installed by checking the version
        # as nvidia-smi is not installed by GPU Operator
        if [ -r /proc/driver/nvidia/version ] || command -v nvidia-smi >/dev/null 2>&1; then
            echo "Detected NVIDIA GPU! Ensuring CUDA drivers are installed..."
            if command -v nvidia-smi >/dev/null 2>&1; then
                echo "Driver version is $(nvidia-smi --query-gpu=driver_version --format=csv,noheader)"
            fi
            if /sbin/ldconfig -p | grep -q libcuda.so.1; then
                echo "CUDA driver library found!"
            else
                echo "WARNING: CUDA driver library (libcuda.so.1) not found in ldconfig cache" >&2
                # Configure dynamic linker for NVIDIA libraries on the GPU K8 host
                nvidia_lib_path="/run/nvidia/driver/usr/lib/x86_64-linux-gnu"
                if [ -d "$nvidia_lib_path" ]; then
                    echo "NVIDIA driver libraries detected, checking ldconfig configuration ..."
                    if [ ! -f "/etc/ld.so.conf.d/nvidia.conf" ] || ! grep -qxF "$nvidia_lib_path" "/etc/ld.so.conf.d/nvidia.conf" 2>/dev/null; then
                        echo "Adding ldconfig path: $nvidia_lib_path"
                        echo "$nvidia_lib_path" >>/etc/ld.so.conf.d/nvidia.conf
                        if ! /sbin/ldconfig; then
                            echo "Failed to update ldconfig cache."
                        fi
                        echo "ldconfig has been set successfully!"
                    else
                        echo "NVIDIA ldconfig path has already been configured."
                    fi
                fi
            fi
        fi
    else
        echo "Detected NVIDIA GPU with GPU Operator layout at /run/driver/nvidia"
        echo "Bind mounting /dev/shm to /run/driver/nvidia/dev/shm..."
        # Bind mount /dev/shm to /run/driver/nvidia/dev/shm
        # This is required for the gpu-controller to work when chrooted into /run/driver/nvidia
        mount --rbind /dev/shm /run/driver/nvidia/dev/shm
        echo "Entering chroot at /run/driver/nvidia to inspect driver..."
        chroot /run/driver/nvidia bash -c <<'END_CHROOT'
            echo "Nvidia Driver version is $(nvidia-smi --query-gpu=driver_version --format=csv,noheader)"
            if /sbin/ldconfig -p | grep -q libcuda.so.1; then
                echo "CUDA driver library found!"
            else
                echo "WARNING: CUDA driver library (libcuda.so.1) not found in chroot ldconfig cache" >&2
            fi
END_CHROOT
        echo "Exited chroot"
    fi
else
    echo "No NVIDIA GPU detected (/proc/driver/nvidia/gpus/ not present), skipping GPU driver check"
fi

# Install all plugins
echo "Plugin build mode: $CEDANA_PLUGINS_BUILDS"
if [[ "$CEDANA_PLUGINS_BUILDS" != "local" ]]; then
    if [[ "$PLUGINS" != "" ]]; then
        echo "Installing plugins:$PLUGINS"
        # shellcheck disable=SC2086
        $APP_PATH plugin install $PLUGINS
        echo "Plugin installation complete"
    else
        echo "No plugins to install"
    fi
    if [[ "$PLUGINS_TO_REMOVE" != "" ]]; then
        echo "Removing plugins: $PLUGINS_TO_REMOVE"
        # shellcheck disable=SC2086
        "$APP_PATH" plugin remove $PLUGINS_TO_REMOVE 2>/dev/null || true
        echo "Plugin removal complete"
    else
        echo "No plugins to remove"
    fi
else
    echo "Local build mode detected, skipping plugin install/remove"
fi

# Improve streaming performance
echo "Configuring pipe settings for streaming performance..."
echo 0 >/proc/sys/fs/pipe-user-pages-soft # change pipe pages soft limit to unlimited
echo 4194304 >/proc/sys/fs/pipe-max-size  # change pipe max size to 4MiB
echo "pipe-user-pages-soft=$(cat /proc/sys/fs/pipe-user-pages-soft) pipe-max-size=$(cat /proc/sys/fs/pipe-max-size)"

#####################################################
# Setup containerd runtime configuration for cedana #
#####################################################

if [ "$ENV" != "production" ]; then
    echo "Non-production environment detected, skipping containerd runtime configuration" >&2
    exit 0
fi

echo "Starting containerd runtime configuration..."

# The CRI plugin config key differs between containerd config version 2 and 3
cedana_runtime_config() {
    if [ "$1" = "3" ]; then
        cat <<END_CAT
[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes."cedana"]
  runtime_type = "io.containerd.runc.v2"
  runtime_path = "${CEDANA_PLUGINS_BIN_DIR}/cedana-shim-runc-v2"
END_CAT
    else
        cat <<END_CAT
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes."cedana"]
  runtime_type = "io.containerd.runc.v2"
  runtime_path = "${CEDANA_PLUGINS_BIN_DIR}/cedana-shim-runc-v2"
END_CAT
    fi
}

CONFIG_CHANGED=false

# k3s and RKE2 manage containerd themselves: <data-dir>/agent/etc/containerd/config.toml
# is regenerated on every service start, so editing it directly is not persistent.
# Instead, both support extending the generated config through a template that
# includes the built-in "base" template: config.toml.tmpl (config version 2) or
# config-v3.toml.tmpl (config version 3, containerd 2.0+, k3s/RKE2 v1.31.6+/v1.32.2+).
#
# The generated config.toml only exists if k3s/RKE2 actually manages containerd;
# with an external containerd (--container-runtime-endpoint) it is never
# generated and templates are never rendered, so fall through to the regular
# containerd flow below in that case.
# Resolve a k3s/RKE2 data dir: --data-dir flag of the running process, then
# config.yaml, then the default location
rancher_data_dir() {
    local name="$1" pid args dir=""
    pid=$(pidof -s "$name" 2>/dev/null || true)
    if [ -n "$pid" ]; then
        args=$(ps -o args= -p "$pid" 2>/dev/null || true)
        dir=$(echo "$args" | sed -n 's/.*--data-dir[= ]\+\([^ ]*\).*/\1/p')
    fi
    if [ -z "$dir" ] && [ -f "/etc/rancher/$name/config.yaml" ]; then
        dir=$(sed -n 's/^[[:space:]]*data-dir:[[:space:]]*//p' "/etc/rancher/$name/config.yaml" | head -n 1 | tr -d '"' | tr -d "'")
    fi
    echo "${dir:-/var/lib/rancher/$name}"
}

RANCHER_SERVICES=""
for RANCHER_NAME in rke2 k3s; do
    RANCHER_DATA_DIR=$(rancher_data_dir "$RANCHER_NAME")
    if [ -f "$RANCHER_DATA_DIR/agent/etc/containerd/config.toml" ]; then
        echo "$RANCHER_NAME node detected (with managed containerd, data dir: $RANCHER_DATA_DIR)"
        RANCHER_CONFIG_DIR="$RANCHER_DATA_DIR/agent/etc/containerd"
        if [ "$RANCHER_NAME" = "rke2" ]; then
            RANCHER_SERVICES="rke2-server rke2-agent"
        else
            RANCHER_SERVICES="k3s k3s-agent"
        fi
        break
    fi
done

if [ -n "$RANCHER_SERVICES" ]; then
    mkdir -p "$RANCHER_CONFIG_DIR"

    # An existing template takes precedence (config-v3.toml.tmpl wins over
    # config.toml.tmpl); otherwise match the version of the generated config.
    if [ -f "$RANCHER_CONFIG_DIR/config-v3.toml.tmpl" ]; then
        TEMPLATE="$RANCHER_CONFIG_DIR/config-v3.toml.tmpl"
        CONTAINERD_VERSION=3
    elif [ -f "$RANCHER_CONFIG_DIR/config.toml.tmpl" ]; then
        TEMPLATE="$RANCHER_CONFIG_DIR/config.toml.tmpl"
        CONTAINERD_VERSION=2
    elif grep -qs 'version = 3' "$RANCHER_CONFIG_DIR/config.toml"; then
        TEMPLATE="$RANCHER_CONFIG_DIR/config-v3.toml.tmpl"
        CONTAINERD_VERSION=3
    else
        TEMPLATE="$RANCHER_CONFIG_DIR/config.toml.tmpl"
        CONTAINERD_VERSION=2
    fi
    echo "Using containerd config template $TEMPLATE (config version $CONTAINERD_VERSION)"

    if [ ! -f "$TEMPLATE" ]; then
        echo '{{ template "base" . }}' >"$TEMPLATE"
    fi

    if ! grep -q 'runtimes."cedana"' "$TEMPLATE"; then
        echo "Adding cedana runtime config to $TEMPLATE"
        {
            echo ""
            cedana_runtime_config "$CONTAINERD_VERSION"
        } >>"$TEMPLATE"
        CONFIG_CHANGED=true
    else
        echo "Cedana runtime config already exists in $TEMPLATE, skipping"
    fi

    RESTART_STAMP_FILES="$TEMPLATE"
else
    CONTAINERD_CONFIG_PATH=${CONTAINERD_CONFIG_PATH:-"/etc/containerd/config.toml"}
    CONTAINERD_CONFD_DIR="$(dirname "$CONTAINERD_CONFIG_PATH")/conf.d"
    echo "Using containerd config path: $CONTAINERD_CONFIG_PATH"

    if [ ! -f "$CONTAINERD_CONFIG_PATH" ]; then
        echo "ERROR: containerd config file not found at $CONTAINERD_CONFIG_PATH" >&2
        exit 1
    fi

    # Detect containerd config version
    if grep -q 'version = 2' "$CONTAINERD_CONFIG_PATH"; then
        CONTAINERD_VERSION=2
    elif grep -q 'version = 3' "$CONTAINERD_CONFIG_PATH"; then
        CONTAINERD_VERSION=3
    else
        echo "ERROR: Unsupported containerd config version. Only version 2 and 3 are supported." >&2
        exit 1
    fi
    echo "Detected containerd config version $CONTAINERD_VERSION"

    if [ "$CONTAINERD_VERSION" = "2" ]; then
        # Version 2: conf.d imports cannot merge multiple runtime tables
        # (https://github.com/containerd/containerd/issues/5837, fixed in v3),
        # so copy the last conf.d file (lexicographically) and append the
        # cedana runtime to the copy.
        LAST_CONFD_FILE=""
        if [ -d "$CONTAINERD_CONFD_DIR" ]; then
            LAST_CONFD_FILE=$(find "$CONTAINERD_CONFD_DIR" -maxdepth 1 -type f -name "*.toml" ! -name "999-cedana.toml" 2>/dev/null | sort | tail -n 1)
        fi

        if [ -n "$LAST_CONFD_FILE" ]; then
            TARGET_CONFIG="$CONTAINERD_CONFD_DIR/999-cedana.toml"
            echo "Basing cedana runtime config on last existing conf.d file: $LAST_CONFD_FILE"
            NEW_CONFIG=$(
                cat "$LAST_CONFD_FILE"
                echo ""
                cedana_runtime_config 2
            )
            if [ "$NEW_CONFIG" != "$(cat "$TARGET_CONFIG" 2>/dev/null)" ]; then
                echo "Writing cedana runtime config to $TARGET_CONFIG"
                echo "$NEW_CONFIG" >"$TARGET_CONFIG"
                CONFIG_CHANGED=true
            else
                echo "Cedana runtime config already up to date in $TARGET_CONFIG, skipping"
            fi
        else
            # Directly add to main config if no conf.d files exist, so that
            # when the NVIDIA plugin is added later it can copy from this and
            # not miss the cedana config.
            TARGET_CONFIG="$CONTAINERD_CONFIG_PATH"
            if ! grep -q 'runtimes."cedana"' "$TARGET_CONFIG"; then
                echo "No conf.d files found, adding cedana runtime config to $TARGET_CONFIG"
                {
                    echo ""
                    cedana_runtime_config 2
                } >>"$TARGET_CONFIG"
                CONFIG_CHANGED=true
            else
                echo "Cedana runtime config already exists in $TARGET_CONFIG, skipping"
            fi
        fi
    else
        # Version 3: conf.d files merge properly, so ship a dedicated conf.d
        # file with only the cedana runtime and make sure it gets imported.
        TARGET_CONFIG="$CONTAINERD_CONFD_DIR/999-cedana.toml"
        mkdir -p "$CONTAINERD_CONFD_DIR"

        if ! grep -qF "$CONTAINERD_CONFD_DIR/*.toml" "$CONTAINERD_CONFIG_PATH"; then
            if grep -q '^imports = \[.*\]' "$CONTAINERD_CONFIG_PATH"; then
                echo "Appending conf.d glob to existing imports in $CONTAINERD_CONFIG_PATH"
                sed -i "s|^imports = \[\(.*\)\]|imports = [\1, \"$CONTAINERD_CONFD_DIR/*.toml\"]|" "$CONTAINERD_CONFIG_PATH"
            elif grep -q '^imports = \[' "$CONTAINERD_CONFIG_PATH"; then
                # Multiline imports array: add the glob right after the opening
                # bracket (TOML allows a trailing comma before the closing one)
                echo "Adding conf.d glob to multiline imports in $CONTAINERD_CONFIG_PATH"
                sed -i "/^imports = \[/a \"$CONTAINERD_CONFD_DIR/*.toml\"," "$CONTAINERD_CONFIG_PATH"
            else
                echo "Adding imports line to $CONTAINERD_CONFIG_PATH"
                sed -i "/^version = 3/a imports = [\"$CONTAINERD_CONFD_DIR/*.toml\"]" "$CONTAINERD_CONFIG_PATH"
            fi
            if ! grep -qF "$CONTAINERD_CONFD_DIR/*.toml" "$CONTAINERD_CONFIG_PATH"; then
                echo "ERROR: Failed to add $CONTAINERD_CONFD_DIR/*.toml to imports in $CONTAINERD_CONFIG_PATH, please add it manually" >&2
                exit 1
            fi
            CONFIG_CHANGED=true
        else
            echo "conf.d glob already present in imports, no changes needed"
        fi

        if ! grep -qs 'runtimes."cedana"' "$TARGET_CONFIG"; then
            echo "Creating cedana runtime config at $TARGET_CONFIG"
            cedana_runtime_config 3 >"$TARGET_CONFIG"
            CONFIG_CHANGED=true
        else
            echo "Cedana runtime config already exists in $TARGET_CONFIG, skipping"
        fi
    fi

    RESTART_STAMP_FILES="$TARGET_CONFIG $CONTAINERD_CONFIG_PATH"
fi

# Determine which service provides containerd on this node
RESTART_SERVICE="containerd"
if [ -n "$RANCHER_SERVICES" ]; then
    RESTART_SERVICE=""
    for SERVICE in $RANCHER_SERVICES; do
        if systemctl is-active --quiet "$SERVICE"; then
            RESTART_SERVICE="$SERVICE"
            break
        fi
    done
    if [ -z "$RESTART_SERVICE" ]; then
        echo "WARNING: No active service found among: $RANCHER_SERVICES; restart it manually to apply the containerd configuration" >&2
        exit 0
    fi
fi

# Epoch time the service's main process started (0 if unknown)
service_started_at() {
    local pid elapsed
    pid=$(systemctl show -p MainPID --value "$1" 2>/dev/null || true)
    if [ -z "$pid" ] || [ "$pid" = "0" ]; then
        echo 0
        return
    fi
    elapsed=$(ps -o etimes= -p "$pid" 2>/dev/null | head -n 1 | tr -d ' ')
    if [ -z "$elapsed" ]; then
        echo 0
        return
    fi
    echo $(($(date +%s) - elapsed))
}

# Even when the config is already up to date, restart if the service hasn't
# started since the config was last written (e.g. a previous run wrote the
# config but its restart failed)
if [ "$CONFIG_CHANGED" = false ]; then
    STARTED_AT=$(service_started_at "$RESTART_SERVICE")
    NEWEST_MTIME=0
    for FILE in $RESTART_STAMP_FILES; do
        [ -f "$FILE" ] || continue
        MTIME=$(stat -c %Y "$FILE")
        if [ "$MTIME" -gt "$NEWEST_MTIME" ]; then
            NEWEST_MTIME=$MTIME
        fi
    done
    if [ "$STARTED_AT" -eq 0 ] || [ "$STARTED_AT" -ge "$NEWEST_MTIME" ]; then
        echo "Containerd runtime configuration already up to date, no restart needed"
        exit 0
    fi
    echo "Containerd runtime configuration up to date but not applied ($RESTART_SERVICE started before it was written)"
fi

# k3s/RKE2 only re-render config.toml from the template on service restart;
# no restart here disrupts running containers
echo "Restarting $RESTART_SERVICE to pick up the cedana runtime configuration..."
if ! systemctl restart "$RESTART_SERVICE"; then
    echo "ERROR: Failed to restart $RESTART_SERVICE, please restart manually" >&2
    exit 1
fi
echo "Restarted $RESTART_SERVICE successfully"
