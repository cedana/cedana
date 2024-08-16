#!/bin/bash

# Install Cedana
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/bin/build-start-daemon.sh /host/build-start-daemon.sh

chroot /host /bin/bash -c '
YUM_PACKAGES="wget git gcc make libnet-devel protobuf \
    protobuf-c protobuf-c-devel protobuf-c-compiler \
    protobuf-compiler protobuf-devel python3-protobuf \
    libnl3-devel libcap-devel libseccomp-devel gpgme-devel btrfs-progs-devel buildah criu"

APT_PACKAGES="wget git make libnl-3-dev libnet-dev \
    libbsd-dev libcap-dev pkg-config \
    libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler pkg-config \
    protobuf-compiler python3-protobuf build-essential \
    libgpgme-dev libseccomp-dev libbtrfs-dev buildah"

install_apt_packages() {
    apt-get update
    for pkg in $APT_PACKAGES; do
        apt-get install -y $pkg || echo "Failed to install $pkg"
    done
}

install_yum_packages() {
    for pkg in $YUM_PACKAGES; do
        yum install -y $pkg || echo "Failed to install $pkg"
    done
    yum group install -y "Development Tools"
}

install_criu_ubuntu_2204() {
    PACKAGE_URL="https://download.opensuse.org/repositories/devel:/tools:/criu/xUbuntu_22.04/amd64/criu_3.19-4_amd64.deb"
    OUTPUT_FILE="criu_3.19-4_amd64.deb"

    wget $PACKAGE_URL -O $OUTPUT_FILE
    dpkg -i $OUTPUT_FILE
    rm $OUTPUT_FILE
}

if [ -f /etc/os-release ]; then
    . /etc/os-release
    case "$ID" in
        debian | ubuntu)
            install_apt_packages
            install_criu_ubuntu_2204
            ;;
        rhel | centos | fedora)
            install_yum_packages
            ;;
        amzn)
            install_yum_packages
            ;;
        *)
            echo "Unknown distribution"
            exit 1
            ;;
    esac
elif [ -f /etc/debian_version ]; then
    install_apt_packages
elif [ -f /etc/redhat-release ]; then
    install_yum_packages
else
    echo "Unknown distribution"
    exit 1
fi


wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz && rm -rf /usr/local/go
tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz && rm go1.22.0.linux-amd64.tar.gz

export PATH=$PATH:/usr/local/go/bin

cd /

./build-start-daemon.sh --systemctl --no-build'
