#!/bin/bash
set -euo pipefail

CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-"release"}
CEDANA_PLUGINS_NATIVE_VERSION=${CEDANA_PLUGINS_NATIVE_VERSION:-"latest"}
CEDANA_PLUGINS_CRIU_VERSION=${CEDANA_PLUGINS_CRIU_VERSION:-"latest"}
CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION=${CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION:-"latest"}
CEDANA_PLUGINS_GPU_VERSION=${CEDANA_PLUGINS_GPU_VERSION:-"latest"}
CEDANA_PLUGINS_STREAMER_VERSION=${CEDANA_PLUGINS_STREAMER_VERSION:-"latest"}
CEDANA_PLUGINS_CRIU_NVIDIA_DEV=${CEDANA_PLUGINS_CRIU_NVIDIA_DEV:-"true"}
CEDANA_PLUGINS_CRIU_INFINIBAND=${CEDANA_PLUGINS_CRIU_INFINIBAND:-"true"}
CEDANA_CHECKPOINT_DIR=${CEDANA_CHECKPOINT_DIR:-"/tmp"}
CEDANA_CHECKPOINT_STREAMS=${CEDANA_CHECKPOINT_STREAMS:-0}

echo "Starting cedana plugin setup"
echo "Config: BUILDS=$CEDANA_PLUGINS_BUILDS NATIVE=$CEDANA_PLUGINS_NATIVE_VERSION CRIU=$CEDANA_PLUGINS_CRIU_VERSION CONTAINERD_RUNTIME=$CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION GPU=$CEDANA_PLUGINS_GPU_VERSION STREAMER=$CEDANA_PLUGINS_STREAMER_VERSION"
echo "Config: CHECKPOINT_DIR=$CEDANA_CHECKPOINT_DIR CHECKPOINT_STREAMS=$CEDANA_CHECKPOINT_STREAMS"
echo "Config: CRIU_NVIDIA_DEV=$CEDANA_PLUGINS_CRIU_NVIDIA_DEV CRIU_INFINIBAND=$CEDANA_PLUGINS_CRIU_INFINIBAND"

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

if [ "$CEDANA_PLUGINS_CRIU_NVIDIA_DEV" = "true" ]; then
    echo "CRIU nvidia-dev plugin enabled, adding to install list"
    PLUGINS="$PLUGINS criu/nvidia-dev@$CEDANA_PLUGINS_CRIU_VERSION"
else
    echo "CRIU nvidia-dev plugin disabled (CEDANA_PLUGINS_CRIU_NVIDIA_DEV=$CEDANA_PLUGINS_CRIU_NVIDIA_DEV), marking for removal"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE criu/nvidia-dev"
fi

if [ "$CEDANA_PLUGINS_CRIU_INFINIBAND" = "true" ]; then
    echo "CRIU infiniband plugin enabled, adding to install list"
    PLUGINS="$PLUGINS criu/infiniband@$CEDANA_PLUGINS_CRIU_VERSION"
else
    echo "CRIU infiniband plugin disabled (CEDANA_PLUGINS_CRIU_INFINIBAND=$CEDANA_PLUGINS_CRIU_INFINIBAND), marking for removal"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE criu/infiniband"
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

CRIU_BIN="$CEDANA_PLUGINS_BIN_DIR/criu"
if [ -x "$CRIU_BIN" ] && command -v ldd >/dev/null 2>&1; then
    CRIU_MISSING_LIBS=$(ldd "$CRIU_BIN" 2>/dev/null | awk '/not found/ {print $1}' | tr '\n' ' ')
    if [ -n "$CRIU_MISSING_LIBS" ]; then
        echo "WARNING: $CRIU_BIN is missing shared libraries: $CRIU_MISSING_LIBS(install them on the node, see install-deps)" >&2
    else
        echo "All shared libraries of $CRIU_BIN are present"
    fi
fi

# Improve streaming performance
echo "Configuring pipe settings for streaming performance..."
echo 0 >/proc/sys/fs/pipe-user-pages-soft # change pipe pages soft limit to unlimited
echo 4194304 >/proc/sys/fs/pipe-max-size  # change pipe max size to 4MiB
echo "pipe-user-pages-soft=$(cat /proc/sys/fs/pipe-user-pages-soft) pipe-max-size=$(cat /proc/sys/fs/pipe-max-size)"
