#!/bin/bash
set -euo pipefail

# Source utils.sh if running as a standalone script (BASH_SOURCE is set)
if [ -n "${BASH_SOURCE[0]:-}" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ -f "$SCRIPT_DIR/utils.sh" ]; then
        source "$SCRIPT_DIR/utils.sh"
    fi
fi

# Installs the host packages cedana needs, and only the missing ones: installed packages are never
# upgraded, and when everything is present the package manager is not run at all.
#
# With CEDANA_INSTALL_DEPS=false, nothing is installed: what is missing is reported, with the
# commands that install it. The k8s helper runs it this way unless k8s.install_deps is set;
# `cedana k8s deps-script` prints this script, to run on the node (e.g. in the node image).
CEDANA_INSTALL_DEPS=${CEDANA_INSTALL_DEPS:-true}

# Define packages for YUM and APT
YUM_PACKAGES=(
    wget git make psmisc
    libnet-devel protobuf-c-devel libnl3-devel libbsd-devel libcap-devel libseccomp-devel gpgme-devel iptables nftables-devel lz4-devel # CRIU
    yq
)

APT_PACKAGES=(
    wget git make psmisc
    libnet1-dev libprotobuf-c-dev libnl-3-dev libbsd-dev libcap-dev libseccomp-dev libgpgme-dev iptables libnftables1 liblz4-dev # CRIU
    sysvinit-utils
)

# yq is needed to configure kubelet, but not available in all distros: a pinned release instead
YQ_VERSION=v4.44.3
case "$(uname -m)" in
    x86_64)
        YQ_BINARY=yq_linux_amd64
        YQ_SHA256=a2c097180dd884a8d50c956ee16a9cec070f30a7947cf4ebf87d5f36213e9ed7
        ;;
    arm64 | aarch64)
        YQ_BINARY=yq_linux_arm64
        YQ_SHA256=0e7e1524f68d91b3ff9b089872d185940ab0fa020a5a9052046ef10547023156
        ;;
    *)
        echo "Unsupported architecture: $(uname -m)"
        exit 1
        ;;
esac
YQ_URL="https://github.com/mikefarah/yq/releases/download/$YQ_VERSION/$YQ_BINARY"

detect_package_manager() {
    local id="" id_like=""
    if [ -f /etc/os-release ]; then
        id=$(. /etc/os-release && echo "${ID:-}")
        id_like=$(. /etc/os-release && echo "${ID_LIKE:-}")
    fi
    case "$id" in
        debian | ubuntu | pop)
            echo apt
            return
            ;;
        rhel | centos | fedora | amzn | rocky | almalinux | ol)
            echo yum
            return
            ;;
    esac
    case " $id_like " in
        *" debian "* | *" ubuntu "*)
            echo apt
            return
            ;;
        *" rhel "* | *" fedora "*)
            echo yum
            return
            ;;
    esac
    if [ -f /etc/debian_version ]; then
        echo apt
    elif [ -f /etc/redhat-release ]; then
        echo yum
    else
        echo "Unknown distribution: ${id:-unknown}" >&2
        return 1
    fi
}

# NOTE: gate on it actually running, not just existing — an interrupted or
# failed download can leave a broken or non-executable binary behind
yq_works() {
    yq --version &>/dev/null
}

PACKAGE_MANAGER=$(detect_package_manager)

MISSING=()
if [ "$PACKAGE_MANAGER" = apt ]; then
    for pkg in "${APT_PACKAGES[@]}"; do
        dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null | grep -q 'install ok installed' || MISSING+=("$pkg")
    done
else
    for pkg in "${YUM_PACKAGES[@]}"; do
        if [ "$pkg" = yq ] && yq_works; then
            continue
        fi
        rpm -q --whatprovides "$pkg" &>/dev/null || MISSING+=("$pkg")
    done
fi

YQ_MISSING=false
yq_works || YQ_MISSING=true

if [ "${#MISSING[@]}" -eq 0 ] && [ "$YQ_MISSING" = false ]; then
    echo "All host dependencies are installed"
    exit 0
fi

if [ "$CEDANA_INSTALL_DEPS" != true ]; then
    {
        echo "WARNING: not installing missing host dependencies (set k8s.install_deps to install them).${MISSING[*]:+ Missing packages: ${MISSING[*]}.}"
        echo "To install them (e.g. in the node image), run 'cedana k8s deps-script' for the full script, or as root:"
        if [ "${#MISSING[@]}" -gt 0 ]; then
            if [ "$PACKAGE_MANAGER" = apt ]; then
                echo "  apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-upgrade ${MISSING[*]}"
            else
                echo "  yum install -y --skip-broken ${MISSING[*]}"
            fi
        fi
        if [ "$YQ_MISSING" = true ]; then
            echo "  wget -q -O /usr/local/bin/yq $YQ_URL"
            echo "  echo '$YQ_SHA256  /usr/local/bin/yq' | sha256sum -c -"
            echo "  chmod +x /usr/local/bin/yq"
        fi
    } >&2
    exit 0
fi

if [ "${#MISSING[@]}" -gt 0 ]; then
    echo "Installing missing packages: ${MISSING[*]}"
    if [ "$PACKAGE_MANAGER" = apt ]; then
        export DEBIAN_FRONTEND=noninteractive
        # Fix any interrupted dpkg state first
        if ! apt-get check &>/dev/null; then
            echo "Fixing interrupted dpkg state..." >&2
            dpkg --configure -a || true
        fi
        apt-get update
        for pkg in "${MISSING[@]}"; do
            if ! apt-get install -y --no-upgrade "$pkg"; then
                echo "Skipping missing package: $pkg" >&2
            fi
        done
    else
        for pkg in "${MISSING[@]}"; do
            if ! yum install -y --skip-broken "$pkg"; then
                echo "Skipping missing package: $pkg" >&2
            fi
        done
    fi
fi

# yum may have installed it
if ! yq_works; then
    # Download to a temp file and only move into place once verified, so a
    # failed download never leaves a broken yq on the node
    YQ_TMP=$(mktemp /usr/local/bin/.yq.XXXXXX)
    trap 'rm -f "$YQ_TMP"' EXIT
    wget -q "$YQ_URL" -O "$YQ_TMP"
    echo "$YQ_SHA256  $YQ_TMP" | sha256sum -c - >/dev/null
    chmod +x "$YQ_TMP"
    "$YQ_TMP" --version >/dev/null
    mv "$YQ_TMP" /usr/local/bin/yq
    trap - EXIT
    echo "yq $YQ_VERSION has been installed"
fi
