#!/bin/bash
set -euo pipefail

CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-"release"}
CEDANA_PLUGINS_NATIVE_VERSION=${CEDANA_PLUGINS_NATIVE_VERSION:-"latest"}
CEDANA_PLUGINS_CRIU_VERSION=${CEDANA_PLUGINS_CRIU_VERSION:-"latest"}
CEDANA_PLUGINS_SLURM_WLM_VERSION=${CEDANA_PLUGINS_SLURM_WLM_VERSION:-"latest"}
CEDANA_PLUGINS_GPU_VERSION=${CEDANA_PLUGINS_GPU_VERSION:-"latest"}
CEDANA_PLUGINS_STREAMER_VERSION=${CEDANA_PLUGINS_STREAMER_VERSION:-"latest"}
CEDANA_CHECKPOINT_DIR=${CEDANA_CHECKPOINT_DIR:-"\tmp"}
CEDANA_CHECKPOINT_STREAMS=${CEDANA_CHECKPOINT_STREAMS:-0}

# Detect the SLURM version string installed on this machine (e.g. "25.11.5").
_detect_slurm_version() {
    local ver=""
    for cmd in sinfo slurmd slurmctld; do
        if command -v "$cmd" &>/dev/null; then
            ver=$(LC_ALL=C "$cmd" --version 2>/dev/null | grep -oP '\d+\.\d+\.\d+' | head -1)
            [[ -n "$ver" ]] && break
        fi
    done
    echo "$ver"
}

# Convert a detected version string (e.g. "25.11.5") to a matching SLURM tag
# from SLURM_VERSIONS via prefix match (e.g. "slurm-25-11-5-1").
# The RPM build-release suffix (-1) is not present in the SLURM version string,
# so prefix matching is required.
_version_to_tag() {
    local ver="$1"
    local prefix
    prefix="slurm-$(echo "$ver" | tr '.' '-')"
    for v in "${SLURM_VERSIONS[@]}"; do
        [[ "$v" == "${prefix}"* ]] && echo "$v" && return
    done
    echo ""
}

# Fetch the latest SLURM WLM version if "latest" is specified
if [[ "$CEDANA_PLUGINS_SLURM_WLM_VERSION" == "latest" ]]; then
    # Get the latest version from cedana plugin list slurm/wlm
    # Example output: "v0.9.291-slurm-25-11-5-1"
    # We extract just the version part: "v0.9.291"
    latest_version=$(cedana plugin list slurm/wlm | awk '/AVAILABLE VERSION/ {getline; print $NF}')
    CEDANA_PLUGINS_SLURM_WLM_VERSION=$(echo "$latest_version" | grep -oP '^v[0-9]+\.[0-9]+\.[0-9]+')
    detected_slurm_version=$(_detect_slurm_version)
    if [ -n "$detected_slurm_version" ]; then
        matching_tag=$(_version_to_tag "$detected_slurm_version")
        if [ -n "$matching_tag" ]; then
            CEDANA_PLUGINS_SLURM_WLM_VERSION="${CEDANA_PLUGINS_SLURM_WLM_VERSION}-${matching_tag}"
        else
            echo "Failed to find a matching SLURM tag for detected version $detected_slurm_version" >&2
            exit 1
        fi
    else
        echo "Failed to detect SLURM version for the slur/wlm plugin" >&2
        exit 1
    fi
fi

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
if [[ "$CEDANA_PLUGINS_BUILDS" != "local" ]]; then
    if [[ "$PLUGINS" != "" ]]; then
        # shellcheck disable=SC2086
        $APP_PATH plugin install $PLUGINS
    fi
    if [[ "$PLUGINS_TO_REMOVE" != "" ]]; then
        # shellcheck disable=SC2086
        "$APP_PATH" plugin remove $PLUGINS_TO_REMOVE 2>/dev/null || true
    fi
fi

# Improve streaming performance
if ! echo 0 >/proc/sys/fs/pipe-user-pages-soft; then
    echo "Warning: Failed to set pipe-user-pages-soft to 0, streaming performance may be degraded" >&2
fi
if ! echo 4194304 >/proc/sys/fs/pipe-max-size; then
    echo "Warning: Failed to set pipe-max-size to 4194304, streaming performance may be degraded" >&2
fi

##########################
# Setup SLURM/WLM plugin #
##########################

if [ -z "${CEDANA_SLURM_NODE_ROLE:-}" ]; then
    echo "Error: CEDANA_SLURM_NODE_ROLE must be set to controller, worker or login" >&2
    exit 1
fi

fuser -k -TERM "${CEDANA_PLUGINS_BIN_DIR}/cedana-slurm" || true
sleep 5

${CEDANA_PLUGINS_BIN_DIR}/cedana-slurm setup --node-role $CEDANA_SLURM_NODE_ROLE
