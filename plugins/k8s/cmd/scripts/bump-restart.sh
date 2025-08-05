#!/bin/bash
# NOTE: This script assumes it's executed in the container environment

set -eo pipefail

# NOTE: The scripts are executed before the binaries, ensure they are copied to the host
# first
mkdir -p /host/cedana /host/cedana/bin /host/cedana/scripts/host /host/cedana/lib
cp -r /scripts/host/* /host/cedana/scripts/host

if [ -f /host/.dockerenv ]; then # for tests
    chroot /host pkill -f 'cedana daemon' || true
else
    chroot /host /bin/bash /cedana/scripts/host/systemd-reset.sh
fi

# Updates the cedana daemon to the latest version
# and restarts using the existing configuration

# We load the binary from docker image for the container
# Copy Cedana binaries and scripts to the host

cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /Makefile /host/cedana/Makefile

cp /usr/local/bin/buildah /host/cedana/bin/buildah
cp /usr/local/bin/netavark /host/cedana/bin/netavark
cp /usr/local/bin/netavark-dhcp-proxy-client /host/cedana/bin/netavark-dhcp-proxy-client

# Allow temporary overrides from environment variables to set specific plugin versions. If not set,
# defaults to latest release versions.

env \
    CEDANA_PLUGINS_BUILDS="$CEDANA_PLUGINS_BUILDS" \
    CEDANA_PLUGINS_NATIVE_VERSION="$CEDANA_PLUGINS_NATIVE_VERSION" \
    CEDANA_PLUGINS_CRIU_VERSION="$CEDANA_PLUGINS_CRIU_VERSION" \
    CEDANA_PLUGINS_K8S_RUNTIME_SHIM_VERSION="$CEDANA_PLUGINS_K8S_RUNTIME_SHIM_VERSION" \
    CEDANA_PLUGINS_GPU_VERSION="$CEDANA_PLUGINS_GPU_VERSION" \
    CEDANA_PLUGINS_STREAMER_VERSION="$CEDANA_PLUGINS_STREAMER_VERSION" \
    chroot /host /bin/bash /cedana/scripts/host/k8s-install-plugins.sh

if [ -f /host/.dockerenv ]; then # for tests
    chroot /host /usr/local/bin/cedana daemon start &> /var/log/cedana-daemon.log &
else
    chroot /host /bin/bash /cedana/scripts/host/systemd-install.sh
fi
