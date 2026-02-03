#!/bin/bash

set -eo pipefail

# get the directory of the script
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE" ]; do
    DIR="$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)"
    SOURCE="$(readlink "$SOURCE")"
    [[ $SOURCE != /* ]] && SOURCE="$DIR/$SOURCE"
done
DIR="$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)"

source "$DIR"/utils.sh

CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-"release"}
CEDANA_PLUGINS_NATIVE_VERSION=${CEDANA_PLUGINS_NATIVE_VERSION:-"latest"}
CEDANA_PLUGINS_CRIU_VERSION=${CEDANA_PLUGINS_CRIU_VERSION:-"latest"}
CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION=${CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION:-"latest"}
CEDANA_PLUGINS_GPU_VERSION=${CEDANA_PLUGINS_GPU_VERSION:-"latest"}
CEDANA_PLUGINS_STREAMER_VERSION=${CEDANA_PLUGINS_STREAMER_VERSION:-"latest"}
CEDANA_CHECKPOINT_STREAMS=${CEDANA_CHECKPOINT_STREAMS:-0}

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
    PLUGINS="$PLUGINS gpu@$CEDANA_PLUGINS_GPU_VERSION"
else
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE gpu"
fi

# check if a storage plugin is required
if [[ "$CEDANA_CHECKPOINT_DIR" == cedana://* ]]; then
    PLUGINS="$PLUGINS storage/cedana@$CEDANA_PLUGINS_NATIVE_VERSION"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/s3 storage/gcs"
elif [[ "$CEDANA_CHECKPOINT_DIR" == s3://* ]]; then
    PLUGINS="$PLUGINS storage/s3@$CEDANA_PLUGINS_NATIVE_VERSION"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/cedana storage/gcs"
elif [[ "$CEDANA_CHECKPOINT_DIR" == gcs://* ]]; then
    PLUGINS="$PLUGINS storage/gcs@$CEDANA_PLUGINS_NATIVE_VERSION"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/cedana storage/s3"
else
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/cedana storage/s3 storage/gcs"
fi

# check if streamer plugin is required
if [ "$CEDANA_CHECKPOINT_STREAMS" -gt 0 ]; then
    PLUGINS="$PLUGINS streamer@$CEDANA_PLUGINS_STREAMER_VERSION"
else
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE streamer"
fi

# If gpu driver present then add gpu plugin
# NOTE: This is no longer used to conditionally add the gpu plugin, but we still
# log the driver version here for informational purposes.
if [ "$ENV" == "k3s" ]; then
    if command -v nvidia-smi >/dev/null 2>&1; then
        echo "Driver version is $(nvidia-smi --query-gpu=driver_version --format=csv,noheader)"
        if /sbin/ldconfig -p | grep -q libcuda.so.1; then
            echo "CUDA driver library found!"
        fi
    fi
elif [ -d /proc/driver/nvidia/gpus/ ]; then
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
            fi
        fi
    else
        echo "Detected NVIDIA GPU! Ensuring CUDA drivers are installed..."
        # Bind mount /dev/shm to /run/nvidia/driver/dev/shm
        # This is required for the gpu-controller to work when chrooted into /run/nvidia/driver path
        mount --rbind /dev/shm /run/nvidia/driver/dev/shm
        chroot /run/driver/nvidia bash -c <<'END_CHROOT'
            echo "Nvidia Driver version is $(nvidia-smi --query-gpu=driver_version --format=csv,noheader)"
            if /sbin/ldconfig -p | grep -q libcuda.so.1; then
                echo "CUDA driver library found!"
            fi
END_CHROOT
    fi
fi

# Install all plugins
if [[ "$CEDANA_PLUGINS_BUILDS" != "local" && "$PLUGINS" != "" ]]; then
    # shellcheck disable=SC2086
    "$APP_PATH" plugin install $PLUGINS

    if [[ "$PLUGINS_TO_REMOVE" != "" ]]; then
        # shellcheck disable=SC2086
        "$APP_PATH" plugin remove $PLUGINS_TO_REMOVE &>/dev/null || true
    fi
fi

# Improve streaming performance
echo 0 >/proc/sys/fs/pipe-user-pages-soft # change pipe pages soft limit to unlimited
echo 4194304 >/proc/sys/fs/pipe-max-size  # change pipe max size to 4MiB

#####################################################
# Setup containerd runtime configuration for cedana #
#####################################################

if [ "$ENV" != "production" ]; then
    echo "Non-production environment detected, skipping containerd runtime configuration"
    exit 0
fi

# k8s path - detect containerd config version
PATH_CONTAINERD_CONFIG=${CONTAINERD_CONFIG_PATH:-"/etc/containerd/config.toml"}

# Detect containerd config version
CONTAINERD_VERSION=""
if grep -q 'version = 2' "$PATH_CONTAINERD_CONFIG"; then
    CONTAINERD_VERSION=2
elif grep -q 'version = 3' "$PATH_CONTAINERD_CONFIG"; then
    CONTAINERD_VERSION=3
else
    echo "ERROR: Unsupported containerd config version. Only version 2 and 3 are supported." >&2
    exit 1
fi

echo "Detected containerd config version $CONTAINERD_VERSION"

CONFD_DIR="/etc/containerd/conf.d"

if [ "$CONTAINERD_VERSION" = "2" ]; then
    # Version 2: Copy last conf.d file (excluding 999-cedana.toml) if exists, then add config
    # This is because merging multiple runtimes in version 2 is problematic
    # See https://github.com/containerd/containerd/issues/5837 (fixed in v3)

    # Find the last .toml file lexicographically (excluding 999-cedana.toml)
    if [ -d "$CONFD_DIR" ]; then
        LAST_CONFD_FILE=$(find "$CONFD_DIR" -maxdepth 1 -type f -name "*.toml" ! -name "999-cedana.toml" 2>/dev/null | sort | tail -n 1)
    else
        LAST_CONFD_FILE=""
    fi

    if [ -n "$LAST_CONFD_FILE" ]; then
        TARGET_CONFIG="$CONFD_DIR/999-cedana.toml"
        echo "Copying existing config from $LAST_CONFD_FILE to $TARGET_CONFIG"
        cp "$LAST_CONFD_FILE" "$TARGET_CONFIG"
        echo "" >>"$TARGET_CONFIG"
    else
        # Directly add to main config if no conf.d files exist, so that when NVIDIA plugin is added
        # later it can copy from this and not miss the cedana config.
        echo "No existing conf.d files found, will directly add to $PATH_CONTAINERD_CONFIG"
        TARGET_CONFIG="$PATH_CONTAINERD_CONFIG"
    fi

    if ! grep -q 'cedana' "$TARGET_CONFIG" 2>/dev/null; then
        echo "Adding cedana runtime config to $TARGET_CONFIG"
        cat >>"$TARGET_CONFIG" <<'END_CAT'
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes."cedana"]
    runtime_type = "io.containerd.runc.v2"
    runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
END_CAT
    else
        echo "Cedana runtime config already exists in $TARGET_CONFIG, skipping"
    fi

elif [ "$CONTAINERD_VERSION" = "3" ]; then
    # Version 3: Ensure imports exist, then create conf.d file with only cedana config
    TARGET_CONFIG="$CONFD_DIR/999-cedana.toml"

    # Ensure conf.d directory exists
    mkdir -p "$CONFD_DIR"

    # Ensure imports line exists in main config
    if ! grep -q 'imports = \[.*"/etc/containerd/conf.d/\*\.toml".*\]' "$PATH_CONTAINERD_CONFIG"; then
        echo "Adding imports to $PATH_CONTAINERD_CONFIG"
        # Check if imports line already exists but doesn't include conf.d
        if grep -q '^imports = \[' "$PATH_CONTAINERD_CONFIG"; then
            # Modify existing imports line to add conf.d
            sed -i 's|^imports = \[\(.*\)\]|imports = [\1, "/etc/containerd/conf.d/*.toml"]|' "$PATH_CONTAINERD_CONFIG"
        else
            # Add imports line at the top after version line
            sed -i '/^version = 3/a imports = ["/etc/containerd/conf.d/*.toml"]' "$PATH_CONTAINERD_CONFIG"
        fi
    fi

    if ! grep -q 'cedana' "$TARGET_CONFIG" 2>/dev/null; then
        echo "Creating cedana runtime config at $TARGET_CONFIG"
        cat >"$TARGET_CONFIG" <<'END_CAT'
[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes."cedana"]
    runtime_type = "io.containerd.runc.v2"
    runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
END_CAT
    else
        echo "Cedana runtime config already exists in $TARGET_CONFIG, skipping"
    fi
fi

echo "Restarting containerd to pick up the new runtime configuration..."
(systemctl restart containerd && echo "Restarted containerd") || echo "Failed to restart containerd, please restart containerd on the node manually to add cedana runtime"
