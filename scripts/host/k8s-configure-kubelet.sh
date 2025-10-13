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
KUBELET_CONFIG_CONTENT_JSON=$(cat <<EOF
{
    "apiVersion": "kubelet.config.k8s.io/v1beta1",
    "kind": "KubeletConfiguration",
    "runtimeRequestTimeout": "$KUBELET_RUNTIME_REQUEST_TIMEOUT"
}
EOF
)

# Function to get the value of a kubelet argument
get_kubelet_arg_value() {
    local arg_name="$1"
    local args="$2"
    # Try to find "--arg_name value" or "--arg_name=value"
    echo "$args" | grep -oP "(?:^|\s)(?:${arg_name}|${arg_name}=)(\S+)" | head -n 1 | sed -E "s/^${arg_name}=?//"
}

# Get kubelet arguments
KUBELET_ARGS=$(ps -o args= -p $(pidof kubelet))
if [ -z "$KUBELET_ARGS" ]; then
    echo "Could not get kubelet arguments. Is kubelet running?" >&2
    exit 1
fi

KUBELET_CONFIG_DIR=$(get_kubelet_arg_value "--config-dir" "$KUBELET_ARGS")
KUBELET_CONFIG_FILE=$(get_kubelet_arg_value "--config" "$KUBELET_ARGS")

if [ -n "$KUBELET_CONFIG_DIR" ]; then
    echo "Found --config-dir: $KUBELET_CONFIG_DIR"
    # Create the directory if it doesn't exist
    mkdir -p "$KUBELET_CONFIG_DIR"
    # Write the kubelet configuration to 99-cedana.conf
    echo "$KUBELET_CONFIG_CONTENT_JSON" > "$KUBELET_CONFIG_DIR/99-cedana.conf"
elif [ -n "$KUBELET_CONFIG_FILE" ]; then
    echo "Found --config: $KUBELET_CONFIG_FILE"
    FILE_EXTENSION="${KUBELET_CONFIG_FILE##*.}"
    TEMP_CONFIG=$(mktemp)

    if [ "$FILE_EXTENSION" == "json" ]; then
        # Merge JSON content using jq
        jq --argjson new_config "$KUBELET_CONFIG_CONTENT_JSON" \
            '. * $new_config' "$KUBELET_CONFIG_FILE" > "$TEMP_CONFIG"
    elif [ "$FILE_EXTENSION" == "yaml" ] || [ "$FILE_EXTENSION" == "yml" ]; then
        # Merge YAML content using yq
        echo "$KUBELET_CONFIG_CONTENT_JSON" | yq eval -P - \
            | yq eval-merge - "$KUBELET_CONFIG_FILE" > "$TEMP_CONFIG"
    else
        echo "Unsupported kubelet configuration file type: $FILE_EXTENSION" >&2
        exit 0
    fi

    # Overwrite the original file with the updated content
    mv "$TEMP_CONFIG" "$KUBELET_CONFIG_FILE"
else
    echo "Neither --config-dir nor --config argument found for kubelet." >&2
    echo "Will not modify kubelet configuration." >&2
    exit 0
fi
