#!/bin/bash
set -euo pipefail

# This is a one-shot script to install the current release version of Cedana.

# VERSION will be automatically injected by the CI workflow
VERSION=${VERSION:-"latest"}
APP_PATH=${APP_PATH:-"/usr/local/bin/cedana"}

# If VERSION doesn't start with 'v' (not a semver tag), set CEDANA_PLUGINS_BUILDS to "alpha"
if [[ ! "$VERSION" =~ ^v ]]; then
    export CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-"alpha"}
else
    export CEDANA_PLUGINS_BUILDS=${CEDANA_PLUGINS_BUILDS:-"release"}
fi

###############################
# Download and install Cedana #
###############################

if [[ "$EUID" -ne 0 ]]; then
    echo "Error: This script must be run as root" >&2
    exit 1
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

curl -fsSL \
    "${CEDANA_URL}"/download?version="$VERSION"\&arch="$ARCH"\&build="$CEDANA_PLUGINS_BUILDS" \
    -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
    -o "$APP_PATH"
chmod +x "$APP_PATH"

# Delete any existing Cedana plugins to avoid incompatibilities with the new version
rm -rf /usr/local/lib/*cedana*

$APP_PATH --merge-config version
