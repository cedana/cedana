#!/bin/bash

chroot /host /bin/bash -c '
#!/bin/bash

# Ensure non-interactive mode for package managers
export DEBIAN_FRONTEND=noninteractive

YUM_PACKAGES=(wget libnet-devel libnl3-devel libcap-devel libseccomp-devel gpgme-devel btrfs-progs-devel buildah criu protobuf protobuf-c protobuf-c-devel protobuf-c-compiler protobuf-compiler protobuf-devel python3-protobuf)

APT_PACKAGES=(wget git make libnl-3-dev libnet-dev libbsd-dev libcap-dev pkg-config libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler pkg-config protobuf-compiler python3-protobuf build-essential libgpgme-dev libseccomp-dev libbtrfs-dev buildah)

install_apt_packages() {
    apt-get update
    apt-get install -y "${APT_PACKAGES[@]}" || echo "Failed to install and setup packages"
}

check_and_install_yum_packages() {
    yum install -y "${YUM_PACKAGES[@]}" || echo "Failed to install and setup packages"
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
            echo "Unknown platform $(uname -m)"
            exit 1
            ;;
    esac

    if ! test -f "$OUTPUT_FILE"; then
        wget "$PACKAGE_URL" -O "$OUTPUT_FILE"
        dpkg -i "$OUTPUT_FILE"
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
            check_and_install_yum_packages
            ;;
        amzn)
            check_and_install_yum_packages
            ;;
        *)
            echo "Unknown distribution"
            exit 1
            ;;
    esac
elif [ -f /etc/debian_version ]; then
    install_apt_packages
elif [ -f /etc/redhat-release ]; then
    check_and_install_yum_packages
else
    echo "Unknown distribution"
    exit 1
fi

systemctl stop cedana.service

rm -rf /var/log/cedana*

'

# Install Cedana
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/bin/build-start-daemon.sh /host/build-start-daemon.sh


chroot /host /bin/bash -c '
cd /
IS_K8S=1 ./build-start-daemon.sh --systemctl --no-build
'

