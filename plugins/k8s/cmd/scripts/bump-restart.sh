#!/bin/bash
# NOTE: This script assumes it's executed in the container environment

set -e

# NOTE: The scripts are executed before the binaries, ensure they are copied to the host
# first
cp -r /scripts/host/* /host/cedana/scripts
chroot /host /bin/bash /cedana/scripts/systemd-reset.sh

# updates the cedana daemon to the latest version
# and restarts with the same arguments

mkdir -p /host/cedana /host/cedana/bin /host/cedana/scripts /host/cedana/lib

# We load the binary from docker image for the container
# Copy Cedana binaries and scripts to the host
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /Makefile /host/cedana/Makefile

cp /usr/local/bin/buildah /host/cedana/bin/buildah
cp /usr/local/bin/netavark /host/cedana/bin/netavark
cp /usr/local/bin/netavark-dhcp-proxy-client /host/cedana/bin/netavark-dhcp-proxy-client

chroot /host /bin/bash /cedana/scripts/k8s-install-plugins.sh # updates to latest
chroot /host /bin/bash /cedana/scripts/systemd-install.sh
