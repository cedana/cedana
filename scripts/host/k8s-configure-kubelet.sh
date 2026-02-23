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

if [ "$ENV" != "production" ]; then
    echo "Running in non-production environment; skipping kubelet configuration update" >&2
    exit 0
fi

# Ensure necessary tools are available
check_tool() {
    command -v "$1" >/dev/null 2>&1
}

if ! check_tool "jq"; then
    echo "Error: 'jq' is not installed. Please install 'jq' to update JSON kubelet configurations." >&2
    exit 1
fi

if ! check_tool "yq"; then
    echo "Error: 'yq' is not installed. Please install 'yq' to update YAML kubelet configurations." >&2
    echo "You can install it from https://github.com/mikefarah/yq#install" >&2
    exit 1
fi

# Detect MicroK8s
IS_MICROK8S=false
if command -v microk8s >/dev/null 2>&1 || [ -d "/var/snap/microk8s" ]; then
    IS_MICROK8S=true
    echo "Detected MicroK8s installation"
fi

# Configure runtimeRequestTimeout to tolerate longer restores
KUBELET_RUNTIME_REQUEST_TIMEOUT="10m"

# The content to be added/updated in the kubelet configuration
KUBELET_CONFIG_CONTENT_JSON=$(
    cat <<EOF
{
    "apiVersion": "kubelet.config.k8s.io/v1beta1",
    "kind": "KubeletConfiguration",
    "runtimeRequestTimeout": "$KUBELET_RUNTIME_REQUEST_TIMEOUT"
}
EOF
)

KUBELET_CONFIG_CONTENT_YAML=$(
    cat <<EOF
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
runtimeRequestTimeout: $KUBELET_RUNTIME_REQUEST_TIMEOUT
EOF
)

get_kubelet_arg_value() {
    local arg="$1"
    shift
    local args=("$@")
    for ((i = 0; i < ${#args[@]}; i++)); do
        case "${args[i]}" in
            $arg=*)
                echo "${args[i]#*=}"
                return
                ;;
            "$arg")
                ((i++))
                echo "${args[i]}"
                return
                ;;
        esac
    done
}

# Handle MicroK8s separately since it uses snap and different paths
if [ "$IS_MICROK8S" = true ]; then
    MICROK8S_KUBELET_ARGS="/var/snap/microk8s/current/args/kubelet"

    if [ ! -f "$MICROK8S_KUBELET_ARGS" ]; then
        echo "ERROR: MicroK8s kubelet args file not found at $MICROK8S_KUBELET_ARGS" >&2
        exit 1
    fi

    echo "Configuring MicroK8s kubelet at $MICROK8S_KUBELET_ARGS"

    # Check if --runtime-request-timeout is already set
    if grep -q -- '--runtime-request-timeout' "$MICROK8S_KUBELET_ARGS"; then
        echo "Updating existing --runtime-request-timeout in $MICROK8S_KUBELET_ARGS"
        sed -i "s/--runtime-request-timeout=[^ ]*/--runtime-request-timeout=$KUBELET_RUNTIME_REQUEST_TIMEOUT/" "$MICROK8S_KUBELET_ARGS"
    else
        echo "Adding --runtime-request-timeout=$KUBELET_RUNTIME_REQUEST_TIMEOUT to $MICROK8S_KUBELET_ARGS"
        echo "--runtime-request-timeout=$KUBELET_RUNTIME_REQUEST_TIMEOUT" >> "$MICROK8S_KUBELET_ARGS"
    fi

    echo "Updated MicroK8s kubelet config:"
    cat "$MICROK8S_KUBELET_ARGS"

    # For MicroK8s, kubelet reads args on startup, so it needs a restart to pick up changes.
    # However, from within a container we may not have access to restart commands.
    # The kubelet will pick up the new config on next MicroK8s restart.
    echo "MicroK8s kubelet config updated. Changes will take effect on next kubelet restart."
    echo "To apply immediately, run on the host: sudo snap restart microk8s"

    # Try to restart if we can, but don't fail if we can't
    {
        if command -v microk8s >/dev/null 2>&1; then
            microk8s stop && microk8s start && echo "MicroK8s restarted successfully"
        elif command -v snap >/dev/null 2>&1; then
            snap restart microk8s && echo "MicroK8s restarted via snap"
        elif systemctl restart snap.microk8s.daemon-kubelet 2>/dev/null; then
            echo "Restarted MicroK8s kubelet via systemctl"
        else
            echo "Note: Automatic restart not available from this context"
        fi
    } || true

    exit 0
fi

# Standard Kubernetes kubelet detection (non-MicroK8s)
KUBELET_PID=$(pidof kubelet || true)
if [ -z "$KUBELET_PID" ]; then
    echo "kubelet is not running" >&2
    exit 1
fi

# Capture full kubelet args as a string
KUBELET_ARGS=$(ps -o args= -p "$KUBELET_PID")
if [ -z "$KUBELET_ARGS" ]; then
    echo "WARNING: Could not get kubelet arguments, please manually modify request timeout" >&2
    exit 0
fi

# Split into proper positional parameters (preserves quoted arguments)
eval "set -- $KUBELET_ARGS"

KUBELET_CONFIG_DIR=$(get_kubelet_arg_value "--config-dir" "$@")
KUBELET_CONFIG_FILE=$(get_kubelet_arg_value "--config" "$@")

if [ -n "$KUBELET_CONFIG_DIR" ]; then
    echo "Found --config-dir: $KUBELET_CONFIG_DIR"
    mkdir -p "$KUBELET_CONFIG_DIR" || {
        echo "Failed to create config dir"
        exit 1
    }
    echo "$KUBELET_CONFIG_CONTENT_JSON" >"$KUBELET_CONFIG_DIR/99-cedana.conf"
    cat "$KUBELET_CONFIG_DIR/99-cedana.conf"
    echo "Wrote config to $KUBELET_CONFIG_DIR/99-cedana.conf"

elif [ -n "$KUBELET_CONFIG_FILE" ]; then
    echo "Found --config: $KUBELET_CONFIG_FILE"
    FILE_EXTENSION="${KUBELET_CONFIG_FILE##*.}"
    TEMP_CONFIG=$(mktemp)

    if [ "$FILE_EXTENSION" == "json" ]; then
        # Merge JSON content safely
        jq -s '.[0] * .[1]' "$KUBELET_CONFIG_FILE" <(echo "$KUBELET_CONFIG_CONTENT_JSON") >"$TEMP_CONFIG"

    elif [[ "$FILE_EXTENSION" =~ ^(yaml|yml)$ ]]; then
        # Merge YAML content safely (yq v4+)
        yq eval-all 'select(fileIndex==0) * select(fileIndex==1)' \
            "$KUBELET_CONFIG_FILE" <(echo "$KUBELET_CONFIG_CONTENT_YAML") >"$TEMP_CONFIG"
    else
        echo "WARNING: Unsupported kubelet configuration file type: $FILE_EXTENSION, skipping kubelet config update" >&2
        exit 0
    fi

    # Overwrite the original file with the updated content
    mv "$TEMP_CONFIG" "$KUBELET_CONFIG_FILE" || {
        echo "Failed to update kubelet config"
        exit 0
    }

    echo "Updated kubelet config at $KUBELET_CONFIG_FILE:"
    cat "$KUBELET_CONFIG_FILE"
else
    echo "WARNING: Neither --config-dir nor --config argument found for kubelet; skipping kubelet config update" >&2
    exit 0
fi

# Restart kubelet to apply changes
success_method=""

if command -v systemctl >/dev/null 2>&1; then
    echo "Attempting to restart kubelet via systemctl..."
    if systemctl restart kubelet; then
        success_method="systemctl"
    else
        echo "systemctl restart failed, trying service and snap"
    fi
fi

if [ -z "$success_method" ] && command -v service >/dev/null 2>&1; then
    echo "Attempting to restart kubelet via service..."
    if service kubelet restart; then
        success_method="service"
    else
        echo "service restart failed, trying snap"
    fi
fi

if [ -z "$success_method" ] && command -v snap >/dev/null 2>&1; then
    echo "Attempting to restart kubelet via snap..."
    if snap restart kubelet-eks; then
        success_method="snap"
    else
        echo "snap restart failed, moving on"
    fi
fi

if [ -z "$success_method" ]; then
    echo "ERROR: Could not restart kubelet via systemctl, service, or snap; please restart manually" >&2
    exit 1
else
    echo "Restart attempts finished; kubelet successfully restarted via $success_method"
fi
