#!/bin/bash

set -eo pipefail

# get the directory of the script
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE"  ]; do
    DIR="$( cd -P "$( dirname "$SOURCE"  )" >/dev/null 2>&1 && pwd  )"
    SOURCE="$(readlink "$SOURCE")"
    [[ $SOURCE != /*  ]] && SOURCE="$DIR/$SOURCE"
done
DIR="$( cd -P "$( dirname "$SOURCE"  )" >/dev/null 2>&1 && pwd  )"

source "$DIR"/utils.sh

CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-"release"}
CEDANA_PLUGINS_NATIVE_VERSION=${CEDANA_PLUGINS_NATIVE_VERSION:-"latest"}
CEDANA_PLUGINS_CRIU_VERSION=${CEDANA_PLUGINS_CRIU_VERSION:-"latest"}
CEDANA_PLUGINS_GPU_VERSION=${CEDANA_PLUGINS_GPU_VERSION:-"latest"}
CEDANA_PLUGINS_STREAMER_VERSION=${CEDANA_PLUGINS_STREAMER_VERSION:-"latest"}
CEDANA_PLUGINS_SLURM_VERSION=${CEDANA_PLUGINS_SLURM_VERSION:-"latest"}
CEDANA_CHECKPOINT_STREAMS=${CEDANA_CHECKPOINT_STREAMS:-0}

PLUGINS=" \
    criu@$CEDANA_PLUGINS_CRIU_VERSION \
    slurm@$CEDANA_PLUGINS_SLURM_VERSION"

# check if a storage plugin is required
if [[ "$CEDANA_CHECKPOINT_DIR" == cedana://* ]]; then
    PLUGINS="$PLUGINS storage/cedana@$CEDANA_PLUGINS_NATIVE_VERSION"
elif [[ "$CEDANA_CHECKPOINT_DIR" == s3://* ]]; then
    PLUGINS="$PLUGINS storage/s3@$CEDANA_PLUGINS_NATIVE_VERSION"
elif [[ "$CEDANA_CHECKPOINT_DIR" == gcs://* ]]; then
    PLUGINS="$PLUGINS storage/gcs@$CEDANA_PLUGINS_NATIVE_VERSION"
fi

# check if streamer plugin is required
if [ "$CEDANA_CHECKPOINT_STREAMS" -gt 0 ]; then
    PLUGINS="$PLUGINS streamer@$CEDANA_PLUGINS_STREAMER_VERSION"
fi

# if gpu driver present then add gpu plugin
if [ "$ENV" == "test" ]; then
    if command -v nvidia-smi >/dev/null 2>&1; then
        PLUGINS="$PLUGINS gpu@$CEDANA_PLUGINS_GPU_VERSION"
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
            PLUGINS="$PLUGINS gpu@$CEDANA_PLUGINS_GPU_VERSION"
            echo "Detected NVIDIA GPU! Ensuring CUDA drivers are installed..."
            if command -v nvidia-smi >/dev/null 2>&1; then
                echo "Driver version is $(nvidia-smi --query-gpu=driver_version --format=csv,noheader)"
            fi
            if /sbin/ldconfig -p | grep -q libcuda.so.1; then
                echo "CUDA driver library found!"
            fi
        fi
    else
        PLUGINS="$PLUGINS gpu@$CEDANA_PLUGINS_GPU_VERSION"
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
    "$APP_PATH" plugin install $PLUGINS
fi

# Improve streaming performance
echo 0 > /proc/sys/fs/pipe-user-pages-soft # change pipe pages soft limit to unlimited
echo 4194304 > /proc/sys/fs/pipe-max-size # change pipe max size to 4MiB

# Add the slurm plugin to SLURM's plugin config
echo "Configuring the Cedana SLURM plugin..."
if ! grep -q "optional /usr/lib64/slurm/libcedana-slurm.so" /etc/slurm/plugstack.conf; then \
        echo "Adding plugin to /etc/slurm/plugstack.conf"; \
        echo "optional /usr/lib64/slurm/libcedana-slurm.so" | sudo tee -a /etc/slurm/plugstack.conf > /dev/null; \
    else \
        echo "Plugin entry already exists in /etc/slurm/plugstack.conf"; \
    fi
