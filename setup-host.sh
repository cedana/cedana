#!/bin/bash

# Define packages for YUM and APT
YUM_PACKAGES=(
    wget git gcc make libnet-devel protobuf protobuf-c protobuf-c-devel protobuf-c-compiler protobuf-compiler protobuf-devel python3-protobuf libnl3-devel
    libcap-devel libseccomp-devel gpgme-devel btrfs-progs-devel buildah libnftables1
)

APT_PACKAGES=(
    wget libgpgme11-dev libseccomp-dev libbtrfs-dev git make libnl-3-dev libnet-dev libbsd-dev libcap-dev libprotobuf-dev python3-protobuf build-essential
    libprotobuf-c1 buildah libnftables1 libelf-dev sysvinit-utils
)

# Function to install APT packages
install_apt_packages() {
    apt-get update
    apt-get install -y "${APT_PACKAGES[@]}" || echo "Failed to install APT packages"
}

# Function to install YUM packages
install_yum_packages() {
    yum install -y "${YUM_PACKAGES[@]}" || echo "Failed to install YUM packages"
}

# Function to install CRIU on Ubuntu 22.04
install_criu_ubuntu_2204() {
    case $(uname -m) in
        x86_64 | amd64)
            TAG=latest
            mkdir -p /cedana/bin
            wget --header "Authorization: Bearer $CEDANA_AUTH_TOKEN" "$CEDANA_URL/k8s/criu/$TAG" -O /cedana/bin/criu
            chmod +x /cedana/bin/criu
            cp /cedana/bin/criu /usr/local/sbin/
            ;;
        aarch64 | arm64)
            PACKAGE_URL="https://download.opensuse.org/repositories/devel:/tools:/criu/xUbuntu_22.04/arm64/criu_4.0-3_arm64.deb"
            OUTPUT_FILE="criu_4.0-3_arm64.deb"
            wget $PACKAGE_URL -O $OUTPUT_FILE
            dpkg -i $OUTPUT_FILE
            rm $OUTPUT_FILE
            ;;
        *)
            echo "Unknown platform architecture $(uname -m)"
            exit 1
            ;;
    esac
}

# Detect OS and install appropriate packages
if [ -f /etc/os-release ]; then
    . /etc/os-release
    case "$ID" in
        debian | ubuntu)
            install_apt_packages
            install_criu_ubuntu_2204
            ;;
        rhel | centos | fedora | amzn)
            install_yum_packages
            ;;
        *)
            echo "Unknown distribution"
            exit 1
            ;;
    esac
elif [ -f /etc/debian_version ]; then
    install_apt_packages
    install_criu_ubuntu_2204
elif [ -f /etc/redhat-release ]; then
    install_yum_packages
else
    echo "Unknown distribution"
    exit 1
fi

GPU=""
if [ -d /proc/driver/nvidia/gpus/ ]; then
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

    echo "Downloading cedana's nvidia interception utilities..."
    mkdir -p /cedana/bin /cedana/lib

    wget --header="Authorization: Bearer $CEDANA_AUTH_TOKEN" -O /cedana/bin/cedana-gpu-controlller $CEDANA_URL/k8s/gpu/gpucontroller
    chmod +x /cedana/bin/cedana-gpu-controlller
    install /cedana/bin/cedana-gpu-controlller /usr/local/bin/cedana-gpu-controlller

    wget --header="Authorization: Bearer $CEDANA_AUTH_TOKEN" -O /cedana/lib/libcedana-gpu.so $CEDANA_URL/k8s/gpu/libcedana
    install /cedana/lib/libcedana-gpu.so /usr/local/lib/libcedana-gpu.so

    wget --header="Authorization: Bearer $CEDANA_AUTH_TOKEN" -O /cedana/bin/containerd-shim-runc-v2 $CEDANA_URL/k8s/cedana-shim/latest
    chmod +x /cedana/bin/containerd-shim-runc-v2

    mkdir -p /usr/local/cedana/bin
    install /cedana/bin/containerd-shim-runc-v2 /usr/local/cedana/bin/containerd-shim-runc-v2

    PATH_CONTAINERD_CONFIG=${CONTAINERD_CONFIG_PATH:-"/etc/containerd/config.toml"}
    if [ ! -f $PATH_CONTAINERD_CONFIG ]; then
        echo "Containerd config file not found at $PATH_CONTAINERD_CONFIG"
        if [ $SEARCH_CONTAINERD_CONFIG -eq 1 ]; then
            echo "Searching for containerd config file..."
            PATH_CONTAINERD_CONFIG=$(find / -fullname **/containerd/config.toml | head -n 1)
            if [ ! -f $PATH_CONTAINERD_CONFIG ]; then
                echo "Containerd config file not found. Exiting..."
                exit 1
            fi
            echo "Found containerd config file at $PATH_CONTAINERD_CONFIG"
        else
            echo "Containerd config file not found. Creating default config file at $PATH_CONTAINERD_CONFIG"
            if [[ $PATH_CONTAINERD_CONFIG == *"k3s"* ]]; then
                echo "k3s detected. Creating default config file at $PATH_CONTAINERD_CONFIG"
                echo '{{ template "base" . }}' > $PATH_CONTAINERD_CONFIG
            else
                echo "" > $PATH_CONTAINERD_CONFIG
            fi
        fi
    fi

    if ! grep -q 'cedana' "$PATH_CONTAINERD_CONFIG"; then
        echo "Writing containerd config to $PATH_CONTAINERD_CONFIG"
        cat >> $PATH_CONTAINERD_CONFIG <<'END_CAT'

        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes."cedana"]
          runtime_type = "io.containerd.runc.v2"
          runtime_path = '/usr/local/cedana/bin/containerd-shim-runc-v2'
END_CAT
    fi

    # SIGHUP is sent to the containerd process to reload the configuration
    echo "Sending SIGHUP to containerd..."
    systemctl restart containerd

    GPU="--gpu"
fi

# create and store startup script for cedana
# this will be used to restart the daemon if it crashes
echo "#!/bin/bash" > /cedana/scripts/run-cedana.sh
echo "/cedana/scripts/build-start-daemon.sh --systemctl --no-build --k8s ${GPU}" >> /cedana/scripts/run-cedana.sh
chmod +x /cedana/scripts/run-cedana.sh

bash /cedana/scripts/run-cedana.sh
