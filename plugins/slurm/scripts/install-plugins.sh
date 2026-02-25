#!/bin/bash
set -euo pipefail

CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-"release"}
CEDANA_PLUGINS_NATIVE_VERSION=${CEDANA_PLUGINS_NATIVE_VERSION:-"latest"}
CEDANA_PLUGINS_CRIU_VERSION=${CEDANA_PLUGINS_CRIU_VERSION:-"latest"}
CEDANA_PLUGINS_SLURM_WLM_VERSION=${CEDANA_PLUGINS_SLURM_WLM_VERSION:-"latest"}
CEDANA_PLUGINS_GPU_VERSION=${CEDANA_PLUGINS_GPU_VERSION:-"latest"}
CEDANA_PLUGINS_STREAMER_VERSION=${CEDANA_PLUGINS_STREAMER_VERSION:-"latest"}
CEDANA_CHECKPOINT_STREAMS=${CEDANA_CHECKPOINT_STREAMS:-0}

# XXX: We always install the GPU plugin for now until auto-detection is added
PLUGINS=" \
    criu@$CEDANA_PLUGINS_CRIU_VERSION \
    slurm/wlm@$CEDANA_PLUGINS_SLURM_WLM_VERSION"

PLUGINS_TO_REMOVE=""

if [ "$CEDANA_PLUGINS_GPU_VERSION" != "none" ]; then
    PLUGINS="$PLUGINS gpu@$CEDANA_PLUGINS_GPU_VERSION"
else
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE gpu"
fi

# check if a storage plugin is required
if [[ "$CEDANA_CHECKPOINT_DIR" == cedana://* ]]; then
    PLUGINS="$PLUGINS storage/cedana@$CEDANA_PLUGINS_NATIVE_VERSION"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/s3 storage/gcs"
elif [[ "$CEDANA_CHECKPOINT_DIR" == s3://* ]]; then
    PLUGINS="$PLUGINS storage/s3@$CEDANA_PLUGINS_NATIVE_VERSION"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/cedana storage/gcs"
elif [[ "$CEDANA_CHECKPOINT_DIR" == gcs://* ]]; then
    PLUGINS="$PLUGINS storage/gcs@$CEDANA_PLUGINS_NATIVE_VERSION"
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/cedana storage/s3"
else
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE storage/cedana storage/s3 storage/gcs"
fi

# check if streamer plugin is required
if [ "$CEDANA_CHECKPOINT_STREAMS" -gt 0 ]; then
    PLUGINS="$PLUGINS streamer@$CEDANA_PLUGINS_STREAMER_VERSION"
else
    PLUGINS_TO_REMOVE="$PLUGINS_TO_REMOVE streamer"
fi

# Install all plugins
if [[ "$CEDANA_PLUGINS_BUILDS" != "local" && "$PLUGINS" != "" ]]; then
    # shellcheck disable=SC2086
    $APP_PATH plugin install $PLUGINS

    if [[ "$PLUGINS_TO_REMOVE" != "" ]]; then
        # shellcheck disable=SC2086
        $APP_PATH plugin remove $PLUGINS_TO_REMOVE || true
    fi
fi

# Improve streaming performance
echo 0 >/proc/sys/fs/pipe-user-pages-soft # change pipe pages soft limit to unlimited
echo 4194304 >/proc/sys/fs/pipe-max-size  # change pipe max size to 4MiB

##########################
# Setup SLURM/WLM plugin #
##########################

if [ "$ENV" != "production" ]; then
    echo "Non-production environment detected, skipping containerd runtime configuration"
    exit 0
fi

cedana-slurm setup
