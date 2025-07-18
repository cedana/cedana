#!/bin/bash
# NOTE: This script assumes it's executed in the container environment

set -e

# NOTE: The scripts are executed before the binaries, ensure they are copied to the host
# first
mkdir -p /host/cedana /host/cedana/bin /host/cedana/scripts/host /host/cedana/lib
cp -r /scripts/host/* /host/cedana/scripts/host

if [ -f /host/.dockerenv ]; then # for tests
    chroot /host pkill -f 'cedana daemon' || true
else
    chroot /host /bin/bash /cedana/scripts/host/systemd-reset.sh
fi

# We load the binary from docker image for the container
# Copy Cedana binaries and scripts to the host

cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/lib/libcedana*.so /host/usr/local/lib/
cp /Makefile /host/cedana/Makefile

cp /usr/local/bin/buildah /host/cedana/bin/buildah
cp /usr/local/bin/netavark /host/cedana/bin/netavark
cp /usr/local/bin/netavark-dhcp-proxy-client /host/cedana/bin/netavark-dhcp-proxy-client

CEDANA_LOG_LEVEL=${CEDANA_LOG_LEVEL:-"info"}
CEDANA_LOG_LEVEL_NO_SERVER=${CEDANA_LOG_LEVEL_NO_SERVER:-"info"}

CEDANA_ADDRESS=${CEDANA_ADDRESS:-"0.0.0.0:8080"}
CEDANA_PROTOCOL=${CEDANA_PROTOCOL:-"tcp"}
CEDANA_DB_REMOTE=${CEDANA_DB_REMOTE:-true}
CEDANA_CLIENT_WAIT_FOR_READY=${CEDANA_CLIENT_WAIT_FOR_READY:-true}

CEDANA_PROFILING_ENABLED=${CEDANA_PROFILING_ENABLED:-false}
CEDANA_METRICS_OTEL=${CEDANA_METRICS_OTEL:-false}

CEDANA_CHECKPOINT_STREAMS=${CEDANA_CHECKPOINT_STREAMS:-0}
CEDANA_CHECKPOINT_COMPRESSION=${CEDANA_CHECKPOINT_COMPRESSION:-"none"}

CEDANA_GPU_POOL_SIZE=${CEDANA_GPU_POOL_SIZE:-1}
CEDANA_GPU_FREEZE_TYPE=${CEDANA_GPU_FREEZE_TYPE:-"IPC"}
CEDANA_GPU_SHM_SIZE=${CEDANA_GPU_SHM_SIZE:-8589934592} # 8GB
CEDANA_GPU_LD_LIB_PATH=${CEDANA_GPU_LD_LIB_PATH:-"/run/nvidia/driver/usr/lib/x86_64-linux-gnu"}

CEDANA_CRIU_MANAGE_CGROUPS=${CEDANA_CRIU_MANAGE_CGROUPS:-"soft"}

CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-"release"}
CEDANA_PLUGINS_NATIVE_VERSION=${CEDANA_PLUGINS_NATIVE_VERSION:-"latest"}
CEDANA_PLUGINS_CRIU_VERSION=${CEDANA_PLUGINS_CRIU_VERSION:-"latest"}
CEDANA_PLUGINS_K8S_RUNTIME_SHIM_VERSION=${CEDANA_PLUGINS_K8S_RUNTIME_SHIM_VERSION:-"latest"}
CEDANA_PLUGINS_GPU_VERSION=${CEDANA_PLUGINS_GPU_VERSION:-"latest"}
CEDANA_PLUGINS_STREAMER_VERSION=${CEDANA_PLUGINS_STREAMER_VERSION:-"latest"}

rm -rf /host/root/.cedana/ # since this is a fresh install

# Enter chroot environment on the host
env \
    CEDANA_LOG_LEVEL="$CEDANA_LOG_LEVEL" \
    CEDANA_LOG_LEVEL_NO_SERVER="$CEDANA_LOG_LEVEL_NO_SERVER" \
    CEDANA_URL="$CEDANA_URL" \
    CEDANA_AUTH_TOKEN="$CEDANA_AUTH_TOKEN" \
    CEDANA_ADDRESS="$CEDANA_ADDRESS" \
    CEDANA_PROTOCOL="$CEDANA_PROTOCOL" \
    CEDANA_DB_REMOTE="$CEDANA_DB_REMOTE" \
    CEDANA_CLIENT_WAIT_FOR_READY="$CEDANA_CLIENT_WAIT_FOR_READY" \
    CEDANA_PROFILING_ENABLED="$CEDANA_PROFILING_ENABLED" \
    CEDANA_METRICS_OTEL="$CEDANA_METRICS_OTEL" \
    CEDANA_CHECKPOINT_STREAMS="$CEDANA_CHECKPOINT_STREAMS" \
    CEDANA_CHECKPOINT_COMPRESSION="$CEDANA_CHECKPOINT_COMPRESSION" \
    CEDANA_GPU_POOL_SIZE="$CEDANA_GPU_POOL_SIZE" \
    CEDANA_GPU_FREEZE_TYPE="$CEDANA_GPU_FREEZE_TYPE" \
    CEDANA_GPU_SHM_SIZE="$CEDANA_GPU_SHM_SIZE" \
    CEDANA_GPU_LD_LIB_PATH="$CEDANA_GPU_LD_LIB_PATH" \
    CEDANA_CRIU_MANAGE_CGROUPS="$CEDANA_CRIU_MANAGE_CGROUPS" \
    CEDANA_PLUGINS_BUILDS="$CEDANA_PLUGINS_BUILDS" \
    CEDANA_PLUGINS_NATIVE_VERSION="$CEDANA_PLUGINS_NATIVE_VERSION" \
    CEDANA_PLUGINS_CRIU_VERSION="$CEDANA_PLUGINS_CRIU_VERSION" \
    CEDANA_PLUGINS_K8S_RUNTIME_SHIM_VERSION="$CEDANA_PLUGINS_K8S_RUNTIME_SHIM_VERSION" \
    CEDANA_PLUGINS_GPU_VERSION="$CEDANA_PLUGINS_GPU_VERSION" \
    CEDANA_PLUGINS_STREAMER_VERSION="$CEDANA_PLUGINS_STREAMER_VERSION" \
    CONTAINERD_CONFIG_PATH="$CONTAINERD_CONFIG_PATH" \
    chroot /host /bin/bash /cedana/scripts/host/k8s-setup-host.sh
