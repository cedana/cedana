set -eo pipefail

if [ -f utils.sh ]; then
    source utils.sh
fi

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
