#!/bin/bash
set -euo pipefail

# Load the binaries and libraries into the host's filesystem
cp --remove-destination $APP_PATH /host/$APP_PATH
cp --remove-destination $CEDANA_PLUGINS_LIB_DIR/libcedana*.so /host/$CEDANA_PLUGINS_LIB_DIR/
cp --remove-destination $CEDANA_PLUGINS_BIN_DIR/*cedana* /host/$CEDANA_PLUGINS_BIN_DIR/

# Stage any plugin artifacts baked into this image onto the host, so that the
# chrooted `plugin install` resolves them locally instead of downloading from
# the registry. CEDANA_PLUGINS_LOCAL_SEARCH_PATH must name this same directory
# (it is read inside the chroot, so no /host prefix there).
CEDANA_PLUGINS_STAGING_DIR=${CEDANA_PLUGINS_STAGING_DIR:-"/opt/cedana/plugins"}
if [ -d "$CEDANA_PLUGINS_STAGING_DIR" ] && [ -n "$(ls -A "$CEDANA_PLUGINS_STAGING_DIR")" ]; then
    echo "Staging baked plugin artifacts from $CEDANA_PLUGINS_STAGING_DIR"
    mkdir -p "/host/$CEDANA_PLUGINS_STAGING_DIR"
    # -p preserves the mode; the local installer copies the source mode verbatim,
    # so binaries must stay 0755 and libraries 0644 all the way to the host.
    cp -p --remove-destination "$CEDANA_PLUGINS_STAGING_DIR"/* "/host/$CEDANA_PLUGINS_STAGING_DIR/"
    ls -l "/host/$CEDANA_PLUGINS_STAGING_DIR"
fi

# Re-initialize config since it's a fresh install
chroot /host $APP_PATH --merge-config version
