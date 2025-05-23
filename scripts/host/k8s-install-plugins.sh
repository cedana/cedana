#!/bin/bash

set -e

# get the directory of the script
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE"  ]; do
    DIR="$( cd -P "$( dirname "$SOURCE"  )" >/dev/null 2>&1 && pwd  )"
    SOURCE="$(readlink "$SOURCE")"
    [[ $SOURCE != /*  ]] && SOURCE="$DIR/$SOURCE"
done
DIR="$( cd -P "$( dirname "$SOURCE"  )" >/dev/null 2>&1 && pwd  )"

source "$DIR"/utils.sh

# NOTE: Assuming Go plugins like k8s, runc, containerd, etc are already built with the image
PLUGINS="criu k8s/runtime-shim" # bare minimum plugins required for k8s

# if gpu driver present then add gpu plugin
if [ -d /proc/driver/nvidia/gpus/ ]; then
    PLUGINS="$PLUGINS gpu"
    echo "Detected NVIDIA GPU! Ensuring CUDA drivers are installed..."
    if [ ! -d /run/driver/nvidia ]; then
        echo "Driver version is $(nvidia-smi --query-gpu=driver_version --format=csv,noheader)"
        if /sbin/ldconfig -p | grep -q libcuda.so.1; then
            echo "CUDA driver library found!"
        fi
    else
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
if [[ -n $PLUGINS ]]; then
    "$APP_PATH" plugin install $PLUGINS
fi

# install the shim configuration to containerd/runtime detected on the host, as it was downlaoded by the k8s plugin
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
        echo "Writing containerd config to $PATH_CONTAINERD_CONFIG"
        cat >> $PATH_CONTAINERD_CONFIG <<'END_CAT'
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes."cedana"]
    runtime_type = "io.containerd.runc.v2"
    runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
END_CAT
    fi
    echo "Sending SIGHUP to containerd..."
    (systemctl restart containerd && echo "Restarted containerd") || echo "Failed to restart containerd, please restart containerd on the node manually to add cedana runtime"
fi
