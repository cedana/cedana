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
            $arg)
                ((i++))
                echo "${args[i]}"
                return
                ;;
        esac
    done
}

if [ -n "$CI" ]; then
    echo "Running in CI; skipping kubelet detection"
    exit 0
fi

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
if command -v systemctl >/dev/null 2>&1; then
    echo "Restarting kubelet via systemctl"
    sudo systemctl restart kubelet || {
        echo "Failed to restart kubelet"
        exit 1
    }
elif command -v service >/dev/null 2>&1; then
    echo "Restarting kubelet via service"
    sudo service kubelet restart || {
        echo "Failed to restart kubelet"
        exit 1
    }
else
    echo "WARNING: Could not find systemctl or service command to restart kubelet; please restart kubelet manually" >&2
    exit 0
fi
