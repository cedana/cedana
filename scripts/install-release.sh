#!/bin/bash
set -euo pipefail

# This is a one-shot script to install the current release version of Cedana.

export APP_NAME="cedana"

# VERSION will be automatically injected by the CI workflow
VERSION=${VERSION:-"latest"}
export APP_PATH="/usr/local/bin/$APP_NAME"
export CEDANA_PLUGINS_LIB_DIR=${CEDANA_PLUGINS_LIB_DIR:-"/usr/local/lib/"}
export CEDANA_PLUGINS_BIN_DIR=${CEDANA_PLUGINS_BIN_DIR:-"/usr/local/bin/"}
export PATH="$CEDANA_PLUGINS_BIN_DIR:$PATH"

# If VERSION doesn't start with 'v' (not a semver tag), set CEDANA_PLUGINS_BUILDS to "alpha"
if [[ ! "$VERSION" =~ ^v ]]; then
    export CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-"alpha"}
else
    export CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-"release"}
fi

# Overrides that should always override the config on installation, even if the
# user has changed them to some other value. For all other config values, the defaults/user-set
# values will be preserved on installation.
export CEDANA_CRIU_MANAGE_CGROUPS="cg_none" # required for how SLURM manages cgroups

###############################
# Download and install Cedana #
###############################

# Try to load values from /etc/cedana/config.json if they're not set in environment
if [[ -f "/etc/cedana/config.json" ]]; then
    if command -v jq &>/dev/null; then
        # Use jq if available
        [[ -z "${CEDANA_URL:-}" ]] && CEDANA_URL=$(jq -r '.connection.url // empty' /etc/cedana/config.json 2>/dev/null || true)
        [[ -z "${CEDANA_AUTH_TOKEN:-}" ]] && CEDANA_AUTH_TOKEN=$(jq -r '.connection.auth_token // empty' /etc/cedana/config.json 2>/dev/null || true)
    else
        # Fallback to grep/sed if jq is not available
        [[ -z "${CEDANA_URL:-}" ]] && CEDANA_URL=$(grep -oP '"url"\s*:\s*"\K[^"]+' /etc/cedana/config.json 2>/dev/null || true)
        [[ -z "${CEDANA_AUTH_TOKEN:-}" ]] && CEDANA_AUTH_TOKEN=$(grep -oP '"auth_token"\s*:\s*"\K[^"]+' /etc/cedana/config.json 2>/dev/null || true)
    fi
else
    # avoid unbound var errors
    export CEDANA_URL="${CEDANA_URL:-}"
    export CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}"
fi

if [[ -z "${CEDANA_URL:-}" ]]; then
    echo "Error: CEDANA_URL environment variable is not set" >&2
    exit 1
fi
if [[ -z "${CEDANA_AUTH_TOKEN:-}" ]]; then
    echo "Error: CEDANA_AUTH_TOKEN environment variable is not set" >&2
    exit 1
fi

ARCH=$(uname -m)
if [[ "$ARCH" == "x86_64" ]]; then
    ARCH="amd64"
elif [[ "$ARCH" == "aarch64" ]]; then
    ARCH="arm64"
else
    echo "Error: Unsupported architecture: $ARCH" >&2
    exit 1
fi

fuser -k -TERM "$APP_PATH" || true
sleep 5

curl -fSL \
    "${CEDANA_URL}"/download?version="$VERSION"\&arch="$ARCH"\&build="$CEDANA_PLUGINS_BUILDS" \
    -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
    -o "$APP_PATH"
chmod +x "$APP_PATH"

# Delete any existing Cedana plugin libs to avoid incompatibilities with the new version
rm -rf ${CEDANA_PLUGINS_LIB_DIR}/libcedana-*.so

# Update /etc/profile.d/cedana.sh with the new environment variables for plugins
PROFILE_D_FILE="/etc/profile.d/cedana.sh"
if ! {
    echo "#!/bin/sh"
    echo "export PATH=${CEDANA_PLUGINS_BIN_DIR}:\$PATH"
    } > "$PROFILE_D_FILE" 2>/dev/null || ! chmod +x "$PROFILE_D_FILE" 2>/dev/null; then
    echo "Warning: Failed to update $PROFILE_D_FILE. Plugin environment variables may need to be set manually." >&2
    echo "Please add the following lines to your shell profile:" >&2
    echo "export PATH=${CEDANA_PLUGINS_BIN_DIR}:\$PATH" >&2
fi

version=$($APP_PATH --merge-config version)
echo "Installed Cedana version $version"
