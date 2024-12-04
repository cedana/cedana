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
if [ -f /proc/driver/nvidia/gpus/ ]; then
    echo "Detected NVIDIA GPU! Ensuring CUDA drivers are installed..."
    if $(/sbin/ldconfig -p | grep -q libcuda.so.1); then
        echo "CUDA drivers found!"
    fi

    set -e
    echo "Downloading cedana's nvidia interception utilities..."

    wget --header="Authorization: Bearer $CEDANA_AUTH_TOKEN" -q -O /cedana/bin/cedana-gpu-controlller $CEDANA_URL/k8s/gpu/cedana-gpu-controller &> /dev/null
    chmod +x /cedana/bin/cedana-gpu-controlller
    install /cedana/bin/cedana-gpu-controlller /usr/local/bin/cedana-gpu-controlller

    wget --header="Authorization: Bearer $CEDANA_AUTH_TOKEN" -q -O /cedana/lib/libcedana-gpu.so $CEDANA_URL/k8s/gpu/libcedana-gpu &> /dev/null
    install /cedana/lib/libcedana-gpu.so /usr/local/lib/libcedana-gpu.so

    wget --header="Authorization: Bearer $CEDANA_AUTH_TOKEN" -q -O /cedana/bin/containerd-shim-runc-v2 $CEDANA_URL/k8s/cedana-shim/latest &> /dev/null
    chmod +x /cedana/bin/containerd-shim-runc-v2

    mkdir -p /usr/local/cedana/bin
    install /cedana/bin/containerd-shim-runc-v2 /usr/local/cedana/bin/containerd-shim-runc-v2

    PATH_CONTAINERD_CONFIG="/etc/containerd/config.toml"
    if [ ! -f $PATH_CONTAINERD_CONFIG ]; then
        echo "Containerd config file not found at $PATH_CONTAINERD_CONFIG"
        echo "Searching for containerd config file..."
        PATH_CONTAINERD_CONFIG=$(find / -fullname **/containerd/config.toml | head -n 1)
        if [ ! -f $PATH_CONTAINERD_CONFIG ]; then
            echo "Containerd config file not found. Exiting..."
            exit 1
        fi
        echo "Found containerd config file at $PATH_CONTAINERD_CONFIG"
    fi

    echo "Writing containerd config to $PATH_CONTAINERD_CONFIG"
    cat <<'END_CAT'
        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes."cedana"]
          runtime_type = "io.containerd.runc.v2"
          runtime_path = '/usr/local/cedana/bin/containerd-shim-runc-v2'
    END_CAT >> $PATH_CONTAINERD_CONFIG

    # SIGHUP is sent to the containerd process to reload the configuration
    echo "Sending SIGHUP to containerd..."
    kill -HUP $(pidof containerd)

    set +e
    GPU="--gpu"
fi

# create and store startup script for cedana
# this will be used to restart the daemon if it crashes
echo "#!/bin/bash" > /cedana/scripts/run-cedana.sh
echo "/cedana/scripts/build-start-daemon.sh --systemctl --no-build --k8s ${GPU}" >> /cedana/scripts/run-cedana.sh
chmod +x /cedana/scripts/run-cedana.sh

bash /cedana/scripts/run-cedana.sh
