#!/bin/bash

# Define packages for YUM and APT
YUM_PACKAGES=(
    wget git gcc make libnet-devel protobuf protobuf-c protobuf-c-devel protobuf-c-compiler protobuf-compiler protobuf-devel python3-protobuf libnl3-devel
    libcap-devel libseccomp-devel gpgme-devel btrfs-progs-devel buildah libnftables1
)

APT_PACKAGES=(
    wget libgpgme11-dev libseccomp-dev libbtrfs-dev git make libnl-3-dev libnet-dev libbsd-dev libcap-dev pkg-config libprotobuf-dev python3-protobuf build-essential
    libprotobuf-c1 buildah libnftables1 libelf-dev
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
            curl -1sLf -O https://dl.cloudsmith.io/$CLOUDSMITH_ENTITLEMENT_TOKEN_CRIU/cedana/criu/raw/versions/$TAG/criu
            cp criu /usr/local/sbin/
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
    echo "nvidia-gpu found!"
    GPU="--gpu"
fi

# create and store startup script for cedana
# this will be used to restart the daemon if it crashes
echo "#!/bin/bash" > /cedana/scripts/run-cedana.sh
echo "/cedana/scripts/build-start-daemon.sh --systemctl --no-build --k8s ${GPU}" >> /cedana/scripts/run-cedana.sh
chmod +x /cedana/scripts/run-cedana.sh

bash /cedana/scripts/run-cedana.sh
