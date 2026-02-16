#!/bin/bash

set -eo pipefail

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
    yq
)

APT_PACKAGES=(
    wget git make
    libnet-dev libprotobuf-c-dev libnl-3-dev libbsd-dev libcap-dev libseccomp-dev libgpgme11-dev libnftables1 # CRIU
    sysvinit-utils
    yq
)

install_apt_packages() {
    # Fix any interrupted dpkg state first
    if ! apt-get check &>/dev/null; then
        echo "Fixing interrupted dpkg state..." >&2
        dpkg --configure -a || true
    fi
    apt-get update
    for pkg in "${APT_PACKAGES[@]}"; do
        if ! apt-get install -y "$pkg"; then
            echo "Skipping missing package: $pkg" >&2
        fi
    done
}

install_yum_packages() {
    for pkg in "${YUM_PACKAGES[@]}"; do
        if ! yum install -y --skip-broken "$pkg"; then
            echo "Skipping missing package: $pkg" >&2
        fi
    done
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

# Hack - yq is needed to configure kubelet, but not available in all distros
bash
case "$(uname -m)" in
    x86_64)
        wget -q https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64 -O /usr/local/bin/yq
        ;;
    arm64 | aarch64)
        wget -q https://github.com/mikefarah/yq/releases/latest/download/yq_linux_arm64 -O /usr/local/bin/yq
        ;;
    *)
        echo "Unsupported architecture: $(uname -m)"
        exit 1
        ;;
esac
chmod +x /usr/local/bin/yq

run_step() {
    local name="$1"
    shift
    echo "=== Running: $name ==="
    if ! "$@"; then
        echo "Step failed: $name " >&2
        exit 1
    fi
    echo "--- Completed: $name ---"
}

CONTAINERD_VERSION="${CONTAINERD_VERSION:-2.1.0}"

upgrade_containerd() {
    local current_version
    current_version=$(containerd --version 2>/dev/null | awk '{print $3}' | sed 's/^v//')

    if [ "$current_version" = "$CONTAINERD_VERSION" ]; then
        echo "containerd is already at version $CONTAINERD_VERSION"
        return 0
    fi

    echo "Upgrading containerd from $current_version to $CONTAINERD_VERSION"

    local arch
    case "$(uname -m)" in
        x86_64)
            arch="amd64"
            ;;
        aarch64 | arm64)
            arch="arm64"
            ;;
        *)
            echo "Unsupported architecture: $(uname -m)" >&2
            return 1
            ;;
    esac

    local tmp_dir
    tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT

    local tarball="containerd-${CONTAINERD_VERSION}-linux-${arch}.tar.gz"
    local url="https://github.com/containerd/containerd/releases/download/v${CONTAINERD_VERSION}/${tarball}"

    echo "Downloading containerd $CONTAINERD_VERSION..."
    if ! curl -fsSL "$url" -o "$tmp_dir/$tarball"; then
        echo "Failed to download containerd" >&2
        return 1
    fi

    echo "Extracting containerd..."
    tar -xzf "$tmp_dir/$tarball" -C "$tmp_dir"

    echo "Stopping containerd, installing binaries, and restarting..."
    systemctl stop containerd
    cp -f "$tmp_dir"/bin/* /usr/bin/
    systemctl start containerd

    # Verify the upgrade
    local new_version
    new_version=$(containerd --version | awk '{print $3}' | sed 's/^v//')
    if [ "$new_version" = "$CONTAINERD_VERSION" ]; then
        echo "Successfully upgraded containerd to $CONTAINERD_VERSION"
    else
        echo "Warning: Expected version $CONTAINERD_VERSION but got $new_version" >&2
        return 1
    fi
}

run_step "upgrade containerd" upgrade_containerd              # upgrade containerd for CDI support
run_step "configure kubelet" "$DIR/k8s-configure-kubelet.sh" # configure kubelet
run_step "install plugins" "$DIR/k8s-install-plugins.sh"     # install the plugins (including shim)
run_step "configure shm" "$DIR/shm-configure.sh"             # configure shm
run_step "configure io_uring" "$DIR/io-uring-configure.sh"   # configure io_uring

if [ "$ENV" != "production" ]; then
    pkill -f 'cedana daemon' || true
    $APP_PATH daemon start &>/var/log/cedana-daemon.log &
else
    "$DIR"/systemd-reset.sh
    "$DIR"/systemd-install.sh
fi
