#!/bin/bash
set -euo pipefail

# Load the binaries and libraries into the host's filesystem
cp --remove-destination $APP_PATH /host/$APP_PATH
cp --remove-destination $CEDANA_PLUGINS_LIB_DIR/libcedana*.so /host/$CEDANA_PLUGINS_LIB_DIR/
cp --remove-destination $CEDANA_PLUGINS_BIN_DIR/*cedana* /host/$CEDANA_PLUGINS_BIN_DIR/

# The host can't resolve cluster DNS, so a .svc URL (an in-cluster spoke) goes in as its address.
# CEDANA_CONNECTION_URL wins over CEDANA_URL in the config, so rewrite the one in effect and set both.
url=${CEDANA_CONNECTION_URL:-${CEDANA_URL:-}}
host=$(sed -E 's#^[a-z]+://([^:/]+).*#\1#' <<<"$url")
if [[ $host == *.svc || $host == *.svc.* ]]; then
    ip=$(getent hosts "$host" | awk '{print $1; exit}')
    [[ -n $ip ]] || { echo "Cannot resolve $host" >&2; exit 1; }
    [[ $ip == *:* ]] && ip="[$ip]" # IPv6 literal in a URL
    export CEDANA_URL=${url/$host/$ip} CEDANA_CONNECTION_URL=${url/$host/$ip}
fi

# Re-initialize config since it's a fresh install
chroot /host $APP_PATH --merge-config version
