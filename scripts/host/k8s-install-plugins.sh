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
CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION=${CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION:-"latest"}
CEDANA_PLUGINS_GPU_VERSION=${CEDANA_PLUGINS_GPU_VERSION:-"latest"}
CEDANA_PLUGINS_STREAMER_VERSION=${CEDANA_PLUGINS_STREAMER_VERSION:-"latest"}
CEDANA_CHECKPOINT_STREAMS=${CEDANA_CHECKPOINT_STREAMS:-0}

PLUGINS=" \
    criu@$CEDANA_PLUGINS_CRIU_VERSION \
    containerd/runtime-runc@$CEDANA_PLUGINS_CONTAINERD_RUNTIME_VERSION \
    containerd@$CEDANA_PLUGINS_NATIVE_VERSION \
    runc@$CEDANA_PLUGINS_NATIVE_VERSION"

PLUGINS_TO_REMOVE=""

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
fi

# if gpu driver present then add gpu plugin
if [ "$ENV" == "k3s" ]; then
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
    # shellcheck disable=SC2086
    "$APP_PATH" plugin install $PLUGINS

    if [[ "$PLUGINS_TO_REMOVE" != "" ]]; then
        # shellcheck disable=SC2086
        "$APP_PATH" plugin remove $PLUGINS_TO_REMOVE &>/dev/null || true
    fi
fi

# Improve streaming performance
echo 0 > /proc/sys/fs/pipe-user-pages-soft # change pipe pages soft limit to unlimited
echo 4194304 > /proc/sys/fs/pipe-max-size # change pipe max size to 4MiB

# install the runtime configuration to containerd/runtime detected on the host, as it was downlaoded by the k8s plugin
if [ -f /var/lib/rancher/k3s/agent/etc/containerd/config.toml ]; then
    PATH_CONTAINERD_CONFIG=/var/lib/rancher/k3s/agent/etc/containerd/config.toml.tmpl
    if ! grep -q 'cedana' "$PATH_CONTAINERD_CONFIG"; then
        echo "k3s detected. Creating default config file at $PATH_CONTAINERD_CONFIG"
        echo '{{ template "base" . }}' > $PATH_CONTAINERD_CONFIG
        cat >> $PATH_CONTAINERD_CONFIG <<'END_CAT'
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes."cedana"]
    runtime_type = "io.containerd.runc.v2"
    runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
END_CAT
    fi
else
    PATH_CONTAINERD_CONFIG=${CONTAINERD_CONFIG_PATH:-"/etc/containerd/config.toml"}
    if ! grep -q 'cedana' "$PATH_CONTAINERD_CONFIG"; then
        # if it's not version = 3 then we assume it's version = 2, as containerd config version = 1 is not used any more, largely that's considered deprecated
        if ! grep -q 'version = 3' "$PATH_CONTAINERD_CONFIG"; then
            echo "Writing containerd config to $PATH_CONTAINERD_CONFIG"
            cat >> "$PATH_CONTAINERD_CONFIG" <<'END_CAT'
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes."cedana"]
    runtime_type = "io.containerd.runc.v2"
    runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
END_CAT
        else
            # TODO: move to python to handle edge cases (this works with AWS from local tests)
            # ideally we should pick up the config for runc and mimic it, cause it might not be default config
            # hence may break compat in some clusters AL2023 and others seem to not have any such issues so can be shipped with just a version check for now
            echo "Writing containerd config to $PATH_CONTAINERD_CONFIG"
            cat >> "$PATH_CONTAINERD_CONFIG" <<'END_CAT'
[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes."cedana"]
    runtime_type = "io.containerd.runc.v2"
    runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
END_CAT
        fi
    fi
    echo "Restarting containerd to pick up the new runtime configuration..."
    (systemctl restart containerd && echo "Restarted containerd") || echo "Failed to restart containerd, please restart containerd on the node manually to add cedana runtime"
fi
