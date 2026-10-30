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
# commands that install it (e.g. to run in the node image). The k8s helper runs it this way when
# k8s.install_deps is false.
CEDANA_INSTALL_DEPS=${CEDANA_INSTALL_DEPS:-true}

# Define packages for YUM and APT
# CRIU's shared libraries (libbsd is in EPEL on RHEL 8/9 and their rebuilds)
YUM_PACKAGES=(
    psmisc procps-ng
    libbsd libselinux gnutls protobuf-c libnl3 libnet libuuid iptables # CRIU
)

# CRIU's other shared libraries (libselinux1, gnutls, libuuid1) are always installed: apt depends on them
APT_PACKAGES=(
    psmisc sysvinit-utils
    libbsd0 libprotobuf-c1 libnl-3-200 libnet1 iptables # CRIU
)

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

PACKAGE_MANAGER=$(detect_package_manager)

MISSING=()
if [ "$PACKAGE_MANAGER" = apt ]; then
    for pkg in "${APT_PACKAGES[@]}"; do
        dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null | grep -q 'install ok installed' || MISSING+=("$pkg")
    done
else
    for pkg in "${YUM_PACKAGES[@]}"; do
        rpm -q --whatprovides "$pkg" &>/dev/null || MISSING+=("$pkg")
    done
fi

if [ "${#MISSING[@]}" -eq 0 ]; then
    echo "All host dependencies are installed"
    exit 0
fi

if [ "$CEDANA_INSTALL_DEPS" != true ]; then
    {
        echo "WARNING: not installing missing host dependencies (k8s.install_deps is false). Missing packages: ${MISSING[*]}."
        echo "To install them (e.g. in the node image), run as root:"
        if [ "$PACKAGE_MANAGER" = apt ]; then
            echo "  apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-upgrade ${MISSING[*]}"
        else
            echo "  yum install -y --skip-broken ${MISSING[*]}"
        fi
    } >&2
    exit 0
fi

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

