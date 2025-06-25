#!/bin/bash

set -e

# get the directory of the script
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE" ]; do
    DIR="$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)"
    SOURCE="$(readlink "$SOURCE")"
    [[ $SOURCE != /* ]] && SOURCE="$DIR/$SOURCE"
done
DIR="$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)"

source "$DIR"/utils.sh

# Define packages for YUM and APT
YUM_PACKAGES=(
    wget git gcc make libnet-devel protobuf protobuf-c protobuf-c-devel protobuf-c-compiler protobuf-compiler protobuf-devel python3-protobuf libnl3-devel
    libcap-devel libseccomp-devel gpgme-devel btrfs-progs-devel buildah libnftables1
)

APT_PACKAGES=(
    wget libgpgme11-dev libseccomp-dev libbtrfs-dev git make libnl-3-dev libnet-dev libbsd-dev libcap-dev libprotobuf-dev python3-protobuf build-essential
    libprotobuf-c1 buildah libnftables1 libelf-dev sysvinit-utils
)

install_apt_packages() {
    apt-get update
    apt-get install -y "${APT_PACKAGES[@]}" || echo "Failed to install APT packages" >&2
}

install_yum_packages() {
    yum install -y "${YUM_PACKAGES[@]}" || echo "Failed to install YUM packages" >&2
}

# Detect OS and install appropriate packages
if [ -f /etc/os-release ]; then
    . /etc/os-release
    case "$ID" in
    debian | ubuntu)
        install_apt_packages
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
elif [ -f /etc/redhat-release ]; then
    install_yum_packages
else
    echo "Unknown distribution"
    exit 1
fi

# FIXME HACK - permanently increase SHM size to ~10GB
# assumes we're chrooted into host
SHM_PATH="/dev/shm"
FSTAB="/etc/fstab"
SIZE_G="10G"
MIN_BYTES=$((10 * 1024 * 1024 * 1024))

# 1. Remount if current size is too small
if [ "$(df --output=size -B 1 "$SHM_PATH" | tail -n 1)" -lt "$MIN_BYTES" ]; then
    echo "Remounting $SHM_PATH with size $SIZE_G..."
    mount -o remount,size=$SIZE_G "$SHM_PATH"
fi

# 2. Ensure fstab is correct for persistence
FSTAB_ENTRY="tmpfs /dev/shm tmpfs defaults,size=$SIZE_G 0 0"
if [ -f "$FSTAB" ] && grep -qE "^\s*[^#]\s*tmpfs\s+$SHM_PATH" "$FSTAB"; then
    sed -i.bak -E "s|^\s*[^#]\s*tmpfs\s+$SHM_PATH.*|$FSTAB_ENTRY|" "$FSTAB"
elif [ -f "$FSTAB" ]; then
    # No entry exists, so append it.
    echo "$FSTAB_ENTRY" >>"$FSTAB"
fi

echo "/dev/shm configuration complete."

"$DIR"/k8s-install-plugins.sh # install the plugins (including shim)

"$DIR"/systemd-reset.sh
"$DIR"/systemd-install.sh
