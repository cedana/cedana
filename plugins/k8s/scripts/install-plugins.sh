#!/bin/bash
set -euo pipefail

check_root

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
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/s3 storage/gcs"
elif [[ "$CEDANA_CHECKPOINT_DIR" == s3://* ]]; then
    echo "S3 storage backend detected, adding storage/s3 plugin"
    PLUGINS="$PLUGINS storage/s3@$CEDANA_PLUGINS_NATIVE_VERSION"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/cedana storage/gcs"
elif [[ "$CEDANA_CHECKPOINT_DIR" == gcs://* ]]; then
    echo "GCS storage backend detected, adding storage/gcs plugin"
    PLUGINS="$PLUGINS storage/gcs@$CEDANA_PLUGINS_NATIVE_VERSION"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/cedana storage/s3"
else
    echo "Local storage backend detected, no storage plugin needed"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/cedana storage/s3 storage/gcs"
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

# k8s path - detect containerd config version
CONTAINERD_CONFIG_PATH=${CONTAINERD_CONFIG_PATH:-"/etc/containerd/config.toml"}
echo "Using containerd config path: $CONTAINERD_CONFIG_PATH"

if [ ! -f "$CONTAINERD_CONFIG_PATH" ]; then
    echo "ERROR: containerd config file not found at $CONTAINERD_CONFIG_PATH" >&2
    exit 1
fi

# Detect containerd config version
CONTAINERD_VERSION=""
if grep -q 'version = 2' "$CONTAINERD_CONFIG_PATH"; then
    CONTAINERD_VERSION=2
elif grep -q 'version = 3' "$CONTAINERD_CONFIG_PATH"; then
    CONTAINERD_VERSION=3
else
    echo "ERROR: Unsupported containerd config version. Only version 2 and 3 are supported." >&2
    exit 1
fi

echo "Detected containerd config version $CONTAINERD_VERSION"

CONTAINERD_CONFD_DIR=${CONTAINERD_CONFD_DIR:-"/etc/containerd/conf.d"}
echo "Using conf.d directory: $CONTAINERD_CONFD_DIR"

if [ "$CONTAINERD_VERSION" = "2" ]; then
    echo "Applying containerd v2 config strategy..."
    # Version 2: Copy last conf.d file (excluding 999-cedana.toml) if exists, then add config
    # This is because merging multiple runtimes in version 2 is problematic
    # See https://github.com/containerd/containerd/issues/5837 (fixed in v3)

    # Find the last .toml file lexicographically (excluding 999-cedana.toml)
    if [ -d "$CONTAINERD_CONFD_DIR" ]; then
        echo "Scanning $CONTAINERD_CONFD_DIR for existing .toml files..."
        LAST_CONFD_FILE=$(find "$CONTAINERD_CONFD_DIR" -maxdepth 1 -type f -name "*.toml" ! -name "999-cedana.toml" 2>/dev/null | sort | tail -n 1)
        echo "Last existing conf.d file: ${LAST_CONFD_FILE:-<none found>}"
    else
        echo "conf.d directory does not exist, skipping scan"
        LAST_CONFD_FILE=""
    fi

    if [ -n "$LAST_CONFD_FILE" ]; then
        TARGET_CONFIG="$CONTAINERD_CONFD_DIR/999-cedana.toml"
        echo "Copying existing config from $LAST_CONFD_FILE to $TARGET_CONFIG"
        cp "$LAST_CONFD_FILE" "$TARGET_CONFIG"
        echo "" >>"$TARGET_CONFIG"
    else
        # Directly add to main config if no conf.d files exist, so that when NVIDIA plugin is added
        # later it can copy from this and not miss the cedana config.
        echo "No existing conf.d files found, will directly add to $CONTAINERD_CONFIG_PATH"
        TARGET_CONFIG="$CONTAINERD_CONFIG_PATH"
    fi

    if ! grep -q 'cedana' "$TARGET_CONFIG" 2>/dev/null; then
        echo "Adding cedana runtime config to $TARGET_CONFIG"
        cat >>"$TARGET_CONFIG" <<'END_CAT'
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes."cedana"]
    runtime_type = "io.containerd.runc.v2"
    runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
END_CAT
        echo "Cedana runtime config written to $TARGET_CONFIG"
    else
        echo "Cedana runtime config already exists in $TARGET_CONFIG, skipping"
    fi

elif [ "$CONTAINERD_VERSION" = "3" ]; then
    echo "Applying containerd v3 config strategy..."
    # Version 3: Ensure imports exist, then create conf.d file with only cedana config
    TARGET_CONFIG="$CONTAINERD_CONFD_DIR/999-cedana.toml"

    # Ensure conf.d directory exists
    echo "Ensuring conf.d directory exists: $CONTAINERD_CONFD_DIR"
    mkdir -p "$CONTAINERD_CONFD_DIR"

    # Ensure imports line exists in main config
    if ! grep -q 'imports = \[.*"/etc/containerd/conf.d/\*\.toml".*\]' "$CONTAINERD_CONFIG_PATH"; then
        echo "conf.d glob not found in imports, updating $CONTAINERD_CONFIG_PATH..."
        # Check if imports line already exists but doesn't include conf.d
        if grep -q '^imports = \[' "$CONTAINERD_CONFIG_PATH"; then
            echo "Existing imports line found, appending conf.d glob"
            sed -i 's|^imports = \[\(.*\)\]|imports = [\1, "/etc/containerd/conf.d/*.toml"]|' "$CONTAINERD_CONFIG_PATH"
        else
            echo "No imports line found, inserting after version = 3"
            sed -i '/^version = 3/a imports = ["/etc/containerd/conf.d/*.toml"]' "$CONTAINERD_CONFIG_PATH"
        fi
        echo "Updated imports in $CONTAINERD_CONFIG_PATH"
    else
        echo "conf.d glob already present in imports, no changes needed"
    fi

    if ! grep -q 'cedana' "$TARGET_CONFIG" 2>/dev/null; then
        echo "Creating cedana runtime config at $TARGET_CONFIG"
        cat >"$TARGET_CONFIG" <<'END_CAT'
[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes."cedana"]
    runtime_type = "io.containerd.runc.v2"
    runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
END_CAT
        echo "Cedana runtime config written to $TARGET_CONFIG"
    else
        echo "Cedana runtime config already exists in $TARGET_CONFIG, skipping"
    fi
fi

echo "Restarting containerd to pick up the new runtime configuration..."
(systemctl restart containerd && echo "Restarted containerd sucessfully") || echo "WARNING: Failed to restart containerd, please restart manually" >&2
