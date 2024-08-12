#!/bin/bash

# Stop Cedana
chroot /host /bin/bash -c '
pkill cdp
pkill otelcol-contrib

systemctl stop cedana.service
'

# Install Cedana
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/bin/build-start-daemon.sh /host/build-start-daemon.sh

chroot /host /bin/bash -c '
#!/bin/bash

# Ensure non-interactive mode for package managers
export DEBIAN_FRONTEND=noninteractive

if [[ $SKIPSETUP -eq 1 ]]; then
    cd /
    IS_K8S=1 ./build-start-daemon.sh --systemctl --no-build
    exit 0
fi

YUM_PACKAGES=(wget libnet-devel libnl3-devel libcap-devel libseccomp-devel gpgme-devel btrfs-progs-devel buildah criu protobuf protobuf-c protobuf-c-devel protobuf-c-compiler protobuf-compiler protobuf-devel python3-protobuf)
APT_PACKAGES=(wget libnl-3-dev libnet-dev libbsd-dev libcap-dev pkg-config libgpgme-dev libseccomp-dev libbtrfs-dev buildah libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler protobuf-compiler python3-protobuf)

check_and_install_apt_packages() {
    apt-get update -y

    local missing_packages=()
    for pkg in "${APT_PACKAGES[@]}"; do
        if ! dpkg -l | grep -qw "$pkg"; then
            missing_packages+=("$pkg")
        fi
    done

    # Check if CRIU is installed
    if ! dpkg -l | grep -qw "criu"; then
        case $(uname -m) in
        x86_64)
            PACKAGE_URL="https://download.opensuse.org/repositories/devel:/tools:/criu/xUbuntu_22.04/amd64/criu_3.19-4_amd64.deb"
            OUTPUT_FILE="criu_3.19-4_amd64.deb"
        aarch64)
            PACKAGE_URL="https://download.opensuse.org/repositories/devel:/tools:/criu/xUbuntu_22.04/arm64/criu_3.19-4_arm64.deb"
            OUTPUT_FILE="criu_3.19-4_arm64.deb"
        *)
            echo "Unknown platform $(uname -m)"
            exit 1
        esac

        wget "$PACKAGE_URL" -O "$OUTPUT_FILE"
        dpkg -i "$OUTPUT_FILE"
    else
        echo "CRIU is already installed"
    fi

    # Install other APT packages if missing
    if [ ${#missing_packages[@]} -gt 0 ]; then
        apt-get install -y -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" "${missing_packages[@]}"
    else
        echo "All APT packages are already installed"
    fi

}

check_and_install_yum_packages() {
    local missing_packages=()
    for pkg in "${YUM_PACKAGES[@]}"; do
        if ! rpm -q "$pkg" &>/dev/null; then
            missing_packages+=("$pkg")
        fi
    done

    # Check if CRIU is installed
    if ! rpm -q "criu" &>/dev/null; then
        yum install -y criu
    else
        echo "CRIU is already installed"
    fi

    # Install other YUM packages if missing
    if [ ${#missing_packages[@]} -gt 0 ]; then
        yum install -y "${missing_packages[@]}"
        yum group install -y "Development Tools"
    else
        echo "All YUM packages are already installed"
    fi
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
            check_and_install_apt_packages
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
    check_and_install_apt_packages
elif [ -f /etc/redhat-release ]; then
    check_and_install_yum_packages
else
    echo "Unknown distribution"
    exit 1
fi

cd /
IS_K8S=1 ./build-start-daemon.sh --systemctl --no-build
'
