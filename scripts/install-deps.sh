#!/bin/bash
set -euo pipefail

# Source utils.sh if running as a standalone script (BASH_SOURCE is set)
if [ -n "${BASH_SOURCE[0]:-}" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ -f "$SCRIPT_DIR/utils.sh" ]; then
        source "$SCRIPT_DIR/utils.sh"
    fi
fi

# Define packages for YUM and APT
YUM_PACKAGES=(
    wget git make psmisc
    libnet-devel protobuf-c-devel libnl3-devel libbsd-devel libcap-devel libseccomp-devel gpgme-devel iptables nftables-devel # CRIU
    yq jq
)

APT_PACKAGES=(
    wget git make psmisc jq
    libnet-dev libprotobuf-c-dev libnl-3-dev libbsd-dev libcap-dev libseccomp-dev libgpgme11-dev iptables libnftables1 # CRIU
    sysvinit-utils
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
        rhel | centos | fedora | amzn | rocky | almalinux | ol)
            install_yum_packages
            ;;
        *)
            case " ${ID_LIKE:-} " in
                *" debian "* | *" ubuntu "*)
                    install_apt_packages
                    ;;
                *" rhel "* | *" fedora "*)
                    install_yum_packages
                    ;;
                *)
                    echo "Unknown distribution: ${ID:-unknown}"
                    exit 1
                    ;;
            esac
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

# Install yq if not already installed
# yq is needed to configure kubelet, but not available in all distros.
# A distro `yq` is often the Python jq-wrapper, which has no `eval-all` and
# silently merges to nothing — so treat that as "not installed" and put the Go
# implementation ahead of it on PATH.
if ! command -v yq &> /dev/null || ! yq --version 2>&1 | grep -qi "mikefarah"; then
    case "$(uname -m)" in
        x86_64)
            YQ_ASSET=yq_linux_amd64
            ;;
        arm64 | aarch64)
            YQ_ASSET=yq_linux_arm64
            ;;
        *)
            echo "Unsupported architecture: $(uname -m)"
            exit 1
            ;;
    esac
    # Download to a temp path first. `wget -O /usr/local/bin/yq` truncates the
    # target before it fetches, so a failed download leaves a 0-byte file that
    # the shell happily "runs" as an empty script — which silently breaks every
    # later yq merge instead of failing here.
    YQ_TEMP=$(mktemp)
    if ! wget -q "https://github.com/mikefarah/yq/releases/latest/download/${YQ_ASSET}" -O "$YQ_TEMP"; then
        rm -f "$YQ_TEMP"
        echo "Error: failed to download yq (${YQ_ASSET})" >&2
        exit 1
    fi
    if [ ! -s "$YQ_TEMP" ]; then
        rm -f "$YQ_TEMP"
        echo "Error: downloaded yq is empty" >&2
        exit 1
    fi
    chmod +x "$YQ_TEMP"
    if ! "$YQ_TEMP" --version 2>&1 | grep -qi "mikefarah"; then
        rm -f "$YQ_TEMP"
        echo "Error: downloaded yq did not report itself as mikefarah/yq" >&2
        exit 1
    fi
    mv "$YQ_TEMP" /usr/local/bin/yq
    echo "yq has been installed"
else
    echo "yq is already installed"
fi