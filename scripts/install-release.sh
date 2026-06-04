#!/bin/bash
set -euo pipefail

# This is a one-shot script to install the current release version of Cedana.

# VERSION will be automatically injected by the CI workflow
VERSION=${VERSION:-"latest"}
if [[ $EUID -ne 0 ]]; then
    export APP_PATH="$HOME/.local/bin/$APP_NAME"
    export CEDANA_PLUGINS_LIB_DIR=${CEDANA_PLUGINS_LIB_DIR:-"$HOME/.local/lib/"}
    export CEDANA_PLUGINS_BIN_DIR=${CEDANA_PLUGINS_BIN_DIR:-"$HOME/.local/bin/"}
else
    export APP_PATH="/usr/local/bin/$APP_NAME"
    export CEDANA_PLUGINS_LIB_DIR=${CEDANA_PLUGINS_LIB_DIR:-"/usr/local/lib/"}
    export CEDANA_PLUGINS_BIN_DIR=${CEDANA_PLUGINS_BIN_DIR:-"/usr/local/bin/"}
fi
export PATH="$CEDANA_PLUGINS_BIN_DIR:$PATH"

# If VERSION doesn't start with 'v' (not a semver tag), set CEDANA_PLUGINS_BUILDS to "alpha"
if [[ ! "$VERSION" =~ ^v ]]; then
    export CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-"alpha"}
else
    export CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-"release"}
fi

###############################
# Download and install Cedana #
###############################

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

curl -fSL \
    "${CEDANA_URL}"/download?version="$VERSION"\&arch="$ARCH"\&build="$CEDANA_PLUGINS_BUILDS" \
    -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
    -o "$APP_PATH"
chmod +x "$APP_PATH"

# Delete any existing Cedana plugin libs to avoid incompatibilities with the new version
rm -rf ${CEDANA_PLUGINS_LIB_DIR}/*cedana*

# Update /etc/profile.d/cedana.sh with the new environment variables for plugins
PROFILE_D_FILE="/etc/profile.d/cedana.sh"
if ! {
    echo "#!/bin/sh"
    echo "export PATH=${CEDANA_PLUGINS_BIN_DIR}:\$PATH"
    echo "export CEDANA_PLUGINS_LIB_DIR=${CEDANA_PLUGINS_LIB_DIR}"
    echo "export CEDANA_PLUGINS_BIN_DIR=${CEDANA_PLUGINS_BIN_DIR}"
    } > "$PROFILE_D_FILE" 2>/dev/null || ! chmod +x "$PROFILE_D_FILE" 2>/dev/null; then
    echo "Warning: Failed to update $PROFILE_D_FILE. Plugin environment variables may need to be set manually." >&2
    echo "Please add the following lines to your shell profile:" >&2
    echo "export PATH=${CEDANA_PLUGINS_BIN_DIR}:\$PATH" >&2
    echo "export CEDANA_PLUGINS_LIB_DIR=${CEDANA_PLUGINS_LIB_DIR}" >&2
    echo "export CEDANA_PLUGINS_BIN_DIR=${CEDANA_PLUGINS_BIN_DIR}" >&2
fi

$APP_PATH --merge-config version
