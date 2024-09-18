#!/bin/bash

chroot /host /bin/bash <<'EOT'
APP_NAME="cedana"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"

echo "Stopping $APP_NAME service..."
$SUDO_USE systemctl stop $APP_NAME.service

# truncate the logs
echo -n > /var/log/cedana-daemon.log
EOT

# Copy Cedana binaries to the host
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/bin/build-start-daemon.sh /host/build-start-daemon.sh

# Enter chroot environment on the host
# TODO NR - CEDANA_URL is a hack, cleanup code to fix
env \
    CEDANA_API_SERVER="$CEDANA_API_SERVER" \
    CEDANA_URL="$CEDANA_API_SERVER" \
    CEDANA_API_KEY="$CEDANA_API_KEY" \
    CEDANA_OTEL_ENABLED="$CEDANA_OTEL_ENABLED" \
    CEDANA_OTEL_PORT="$CEDANA_OTEL_PORT" \
    chroot /host /bin/bash <<'EOT'

if [[ $SKIPSETUP -eq 1 ]]; then
    cd /
    CEDANA_URL="$CEDANA_API_SERVER" CEDANA_API_KEY="$CEDANA_API_KEY" ./build-start-daemon.sh --systemctl --no-build --otel --k8s
    exit 0
fi

# Define packages for YUM and APT
YUM_PACKAGES=(
    wget git gcc make libnet-devel protobuf protobuf-c protobuf-c-devel
    protobuf-c-compiler protobuf-compiler protobuf-devel python3-protobuf
    libnl3-devel libcap-devel libseccomp-devel gpgme-devel btrfs-progs-devel
    buildah criu
)

APT_PACKAGES=(
    wget libgpgme11-dev libseccomp-dev libbtrfs-dev git make
    libnl-3-dev libnet-dev libbsd-dev libcap-dev pkg-config
    libprotobuf-dev python3-protobuf build-essential libprotobuf-c1 buildah
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
    PACKAGE_URL="https://download.opensuse.org/repositories/devel:/tools:/criu/xUbuntu_22.04/amd64/criu_3.19-4_amd64.deb"
    OUTPUT_FILE="criu_3.19-4_amd64.deb"

    wget $PACKAGE_URL -O $OUTPUT_FILE
    dpkg -i $OUTPUT_FILE
    rm $OUTPUT_FILE
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

# Run the Cedana daemon setup script
cd /
./build-start-daemon.sh --systemctl --no-build --k8s

EOT
