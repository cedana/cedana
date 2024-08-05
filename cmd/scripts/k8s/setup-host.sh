#!/bin/bash

# Install Cedana
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/bin/build-start-daemon.sh /host/build-start-daemon.sh

chroot /host <<"EOT"

if [[ $SKIPSETUP -eq 1 ]]; then
    cd /
    IS_K8S=1 ./build-start-daemon.sh --systemctl --no-build
    exit 0
fi

YUM_PACKAGES=(wget libnet-devel libnl3-devel libcap-devel libseccomp-devel gpgme-devel btrfs-progs-devel buildah criu)
APT_PACKAGES=(wget libnl-3-dev libnet-dev libbsd-dev libcap-dev pkg-config libgpgme-dev libseccomp-dev libbtrfs-dev buildah)

install_apt_packages() {
    apt-get update

    apt-get install -y -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" golang-github-containers-image golang-github-containers-common

    # install all packages at once
    apt-get install -y "${APT_PACKAGES[@]}" || echo "Failed to install $pkg"
}

install_yum_packages() {
    yum install -y "${YUM_PACKAGES[@]}"
    yum group install -y "Development Tools"
}

install_criu_ubuntu_2204() {
    case $(uname -m) in
        x86 | x86_64)
            PACKAGE_URL="https://download.opensuse.org/repositories/devel:/tools:/criu/xUbuntu_22.04/amd64/criu_3.19-4_amd64.deb"
            OUTPUT_FILE="criu_3.19-4_amd64.deb"
            ;;
        armv7 | aarch64)
            PACKAGE_URL="https://download.opensuse.org/repositories/devel:/tools:/criu/xUbuntu_22.04/arm64/criu_3.19-4_arm64.deb"
            OUTPUT_FILE="criu_3.19-4_arm64.deb"
            ;;
        *)
            echo "Unknown platform " $(uname -m)
            exit 1
            ;;
    esac

    if ! test -f $OUTPUT_FILE; then
        wget $PACKAGE_URL -O $OUTPUT_FILE
        dpkg -i $OUTPUT_FILE
    fi
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


cd /
IS_K8S=1 ./build-start-daemon.sh --systemctl --no-build

EOT

