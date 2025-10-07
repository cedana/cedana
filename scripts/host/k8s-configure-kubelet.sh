#!/bin/bash

set -eo pipefail

# get the directory of the script
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE" ]; do
    DIR="$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)"
    SOURCE="$(readlink "$SOURCE")"
    [[ $SOURCE != /* ]] && SOURCE="$DIR/$SOURCE"
done
DIR="$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)"

source "$DIR"/utils.sh

# Configure runtimeRequestTimeout to tolerate longer restores

KUBELET_RUNTIME_REQUEST_TIMEOUT="10m"

# EKS
KUBELET_CONFIG_DIR_EKS="/etc/kubernetes/kubelet/config.json.d"
KUBELET_CONFIG_FILE_EKS="$KUBELET_CONFIG_DIR_EKS/99-cedana.conf"
KUBELET_CONFIG_CONTENT_JSON=$(cat <<EOF
{
    "apiVersion": "kubelet.config.k8s.io/v1beta1",
    "kind": "KubeletConfiguration",
    "runtimeRequestTimeout": "$KUBELET_RUNTIME_REQUEST_TIMEOUT"
}
EOF
)

# GKE
KUBELET_CONFIG_FILE_GKE="/home/kubernetes/kubelet-config.yaml"

if [ -d "${KUBELET_CONFIG_DIR_EKS}" ]; then
    echo "Detected EKS kubelet config directory '${KUBELET_CONFIG_DIR_EKS}'"
    if [ -f "${KUBELET_CONFIG_FILE_EKS}" ]; then
        echo "Removing existing kubelet config file '${KUBELET_CONFIG_FILE_EKS}'"
        rm -f "${KUBELET_CONFIG_FILE_EKS}" || (echo "Failed to remove existing config file" >&2 && exit 1)
    else
        echo "Creating new kubelet config file '${KUBELET_CONFIG_FILE_EKS}'"
    fi
    echo "Using kubelet config:"
    echo "${KUBELET_CONFIG_CONTENT}"
    echo "${KUBELET_CONFIG_CONTENT}" > "${KUBELET_CONFIG_FILE_EKS}"
elif [ -f "${KUBELET_CONFIG_FILE_GKE}" ]; then
    echo "Detected GKE kubelet config file '${KUBELET_CONFIG_FILE_GKE}'"
    echo "Backing up existing kubelet config file to '${KUBELET_CONFIG_FILE_GKE}.bak'"
    cp -f "${KUBELET_CONFIG_FILE_GKE}" "${KUBELET_CONFIG_FILE_GKE}.bak" || (echo "Failed to back up existing config file" >&2 && exit 1)
    echo "Modifying kubelet config file to set runtimeRequestTimeout to '$KUBELET_RUNTIME_REQUEST_TIMEOUT'"
    yq eval ".runtimeRequestTimeout = \"$KUBELET_RUNTIME_REQUEST_TIMEOUT\"" -i "${KUBELET_CONFIG_FILE_GKE}" || (echo "Failed to modify kubelet config file" >&2 && exit 1)
else
    echo "No known kubelet config file or directory found. Please configure kubelet manually to set runtimeRequestTimeout to '$KUBELET_RUNTIME_REQUEST_TIMEOUT'." >&2
    exit 0
fi

# Restart kubelet to apply changes
if command -v systemctl >/dev/null 2>&1; then
    echo "Restarting kubelet service"
    systemctl daemon-reload || (echo "Failed to reload systemd daemon" >&2 && exit 1)
    systemctl restart kubelet || (echo "Failed to restart kubelet service" >&2 && exit 1)
elif command -v service >/dev/null 2>&1; then
    echo "Restarting kubelet service"
    service kubelet restart || (echo "Failed to restart kubelet service" >&2 && exit 1)
else
    echo "Neither systemctl nor service command found. Please restart kubelet manually to apply config changes." >&2
fi
