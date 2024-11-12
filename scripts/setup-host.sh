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

source $DIR/utils.sh

# Set env vars
CEDANA_METRICS_ASR=true
CEDANA_METRICS_OTEL_ENABLED=true

PLUGINS="runc" # bare minimum plugin
# if gpu driver present then enable gpu plugin
if command -v nvidia-smi &>/dev/null; then
    echo "nvidia-smi found! CUDA Version: $(nvidia-smi --version | grep CUDA | cut -d ':' -f 2)"
    PLUGINS=$PLUGINS,gpu
fi

if [[ $SKIPSETUP -eq 1 ]]; then
    exec $DIR/start-systemd.sh --plugins=$PLUGINS
fi

# Define packages for YUM and APT
YUM_PACKAGES=(
    wget git gcc make libnet-devel protobuf protobuf-c protobuf-c-devel protobuf-c-compiler protobuf-compiler protobuf-devel python3-protobuf libnl3-devel
    libcap-devel libseccomp-devel gpgme-devel btrfs-progs-devel buildah criu libnftables1
)

APT_PACKAGES=(
    wget libgpgme11-dev libseccomp-dev libbtrfs-dev git make libnl-3-dev libnet-dev libbsd-dev libcap-dev pkg-config libprotobuf-dev python3-protobuf build-essential
    libprotobuf-c1 buildah libnftables1 libelf-dev
)

install_apt_packages() {
    apt-get update
    apt-get install -y "${APT_PACKAGES[@]}" || echo "Failed to install APT packages" >&2
}

install_yum_packages() {
    yum install -y "${YUM_PACKAGES[@]}" || echo "Failed to install YUM packages" >&2
}

install_criu_ubuntu_2204() {
    case $(uname -m) in
        x86_64 | amd64)
            package_url="https://download.opensuse.org/repositories/devel:/tools:/criu/xUbuntu_22.04/amd64/criu_4.0-3_amd64.deb"
            output_file="criu_4.0-3_amd64.deb"
            ;;
        aarch64 | arm64)
            package_url="https://download.opensuse.org/repositories/devel:/tools:/criu/xUbuntu_22.04/arm64/criu_4.0-3_arm64.deb"
            output_file="criu_4.0-3_arm64.deb"
            ;;
        *)
            echo "Unknown platform architecture $(uname -m)"
            exit 1
            ;;
    esac

    wget $package_url -O $output_file
    dpkg -i $output_file
    rm $output_file
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

$DIR/start-systemd.sh --plugins=$PLUGINS
