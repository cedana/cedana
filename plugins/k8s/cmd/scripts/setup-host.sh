#!/bin/bash
# NOTE: This script assumes it's executed in the container environment

set -e

# NOTE: The scripts are executed before the binaries, ensure they are copied to the host
# first
mkdir -p /host/cedana /host/cedana/bin /host/cedana/scripts /host/cedana/lib
cp -r /scripts/host/* /host/cedana/scripts
chroot /host /bin/bash /cedana/scripts/systemd-reset.sh

# We load the binary from docker image for the container
# Copy Cedana binaries and scripts to the host
cp /usr/local/bin/cedana /host/usr/local/bin/cedana
cp /usr/local/lib/libcedana*.so /host/usr/local/lib/
cp /Makefile /host/cedana/Makefile

cp /usr/local/bin/buildah /host/cedana/bin/buildah
cp /usr/local/bin/netavark /host/cedana/bin/netavark
cp /usr/local/bin/netavark-dhcp-proxy-client /host/cedana/bin/netavark-dhcp-proxy-client

CEDANA_REMOTE=${CEDANA_REMOTE:-"true"}
CEDANA_WAIT_FOR_READY=${CEDANA_WAIT_FOR_READY:-"true"}
CEDANA_ADDRESS=${CEDANA_ADDRESS:-"0.0.0.0:8080"}
CEDANA_PROTOCOL=${CEDANA_PROTOCOL:-"tcp"}

# Enter chroot environment on the host
env \
    CEDANA_URL="$CEDANA_URL" \
    CEDANA_AUTH_TOKEN="$CEDANA_AUTH_TOKEN" \
    CEDANA_METRICS_ASR="$CEDANA_METRICS_ASR" \
    CEDANA_METRICS_OTEL="$CEDANA_METRICS_OTEL" \
    CEDANA_LOG_LEVEL="$CEDANA_LOG_LEVEL" \
    CONTAINERD_CONFIG_PATH="$CONTAINERD_CONFIG_PATH" \
    CEDANA_REMOTE="$CEDANA_REMOTE" \
    CEDANA_WAIT_FOR_READY="$CEDANA_WAIT_FOR_READY" \
    CEDANA_ADDRESS="$CEDANA_ADDRESS" \
    CEDANA_PROTOCOL="$CEDANA_PROTOCOL" \
    chroot /host /bin/bash /cedana/scripts/k8s-setup-host.sh
