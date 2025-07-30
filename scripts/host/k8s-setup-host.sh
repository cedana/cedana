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
    wget git make
    libnet-devel protobuf-c-devel libnl3-devel libbsd-devel libcap-devel libseccomp-devel gpgme-devel nftables-devel # CRIU
    buildah
)

APT_PACKAGES=(
    wget git make
    libnet-dev libprotobuf-c-dev libnl-3-dev libbsd-dev libcap-dev libseccomp-dev libgpgme11-dev libnftables1 # CRIU
    buildah
    sysvinit-utils
)

install_apt_packages() {
    apt-get update
    apt-get install -y "${APT_PACKAGES[@]}" || echo "Failed to install APT packages" >&2
}

install_yum_packages() {
    yum install -y --skip-broken "${YUM_PACKAGES[@]}" || echo "Failed to install YUM packages" >&2
}

# Detect OS and install appropriate packages
if [ -f /etc/os-release ]; then
    . /etc/os-release
    case "$ID" in
    debian | ubuntu | pop)
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

"$DIR"/k8s-install-plugins.sh # install the plugins (including shim)

if [ -f /.dockerenv ]; then # for tests
    pkill -f 'cedana daemon' || true
    $APP_PATH daemon start &> /var/log/cedana-daemon.log &
else
    "$DIR"/systemd-reset.sh
    "$DIR"/systemd-install.sh
fi
