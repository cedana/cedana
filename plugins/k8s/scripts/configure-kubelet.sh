#!/bin/bash
set -euo pipefail

check_root

if [ "$ENV" != "production" ]; then
    echo "Running in non-production environment; skipping kubelet configuration update" >&2
    exit 0
fi

# Ensure necessary tools are available
check_tool() {
    command -v "$1" >/dev/null 2>&1
}

echo "Checking required tools..."

if ! check_tool "jq"; then
    echo "Error: 'jq' is not installed. Please install 'jq' to update JSON kubelet configurations." >&2
    exit 1
fi
echo "jq found at: $(command -v jq) ($(jq --version))"

if ! check_tool "yq"; then
    echo "Error: 'yq' is not installed. Please install 'yq' to update YAML kubelet configurations." >&2
    echo "You can install it from https://github.com/mikefarah/yq#install" >&2
    exit 1
fi
echo "yq found at: $(command -v yq) ($(yq --version))"

# Configure runtimeRequestTimeout to tolerate longer restores
KUBELET_RUNTIME_REQUEST_TIMEOUT="10m"
echo "Target runtimeRequestTimeout: $KUBELET_RUNTIME_REQUEST_TIMEOUT"

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

echo "Looking for running kubelet process..."
KUBELET_PID=$(pidof kubelet || true)
if [ -z "$KUBELET_PID" ]; then
    echo "Error: kubelet is not running" >&2
    exit 1
fi
echo "Found kubelet process (PID=$KUBELET_PID)"

echo "Reading kubelet arguments..."
KUBELET_ARGS=$(ps -o args= -p "$KUBELET_PID")
if [ -z "$KUBELET_ARGS" ]; then
    echo "WARNING: Could not get kubelet arguments, please manually modify request timeout" >&2
    exit 0
fi
echo "Raw kubelet args: $KUBELET_ARGS"

# Split into proper positional parameters (preserves quoted arguments)
eval "set -- $KUBELET_ARGS"

KUBELET_CONFIG_DIR=$(get_kubelet_arg_value "--config-dir" "$@")
KUBELET_CONFIG_FILE=$(get_kubelet_arg_value "--config" "$@")

echo "Resolved --config-dir: ${KUBELET_CONFIG_DIR:-<not set>}"
echo "Resolved --config:     ${KUBELET_CONFIG_FILE:-<not set>}"

if [ -n "$KUBELET_CONFIG_DIR" ]; then
    TARGET="$KUBELET_CONFIG_DIR/99-cedana.conf"
    echo "Strategy: drop-in config dir, writing to $TARGET"

    echo "Ensuring config dir exists: $KUBELET_CONFIG_DIR"
    mkdir -p "$KUBELET_CONFIG_DIR" || {
        echo "Error: Failed to create config dir: $KUBELET_CONFIG_DIR" >&2
        exit 1
    }

    echo "$KUBELET_CONFIG_CONTENT_JSON" >"$TARGET"
    echo "Wrote config to $TARGET:"
    cat "$TARGET"

elif [ -n "$KUBELET_CONFIG_FILE" ]; then
    echo "Strategy: merge into existing config file at $KUBELET_CONFIG_FILE"

    if [ ! -f "$KUBELET_CONFIG_FILE" ]; then
        echo "Error: Kubelet config file not found: $KUBELET_CONFIG_FILE" >&2
        exit 1
    fi

    FILE_EXTENSION="${KUBELET_CONFIG_FILE##*.}"
    echo "Detected config file extension: .$FILE_EXTENSION"
    TEMP_CONFIG=$(mktemp)
    echo "Created temp file for merge: $TEMP_CONFIG"

    if [ "$FILE_EXTENSION" == "json" ]; then
        echo "Merging JSON config..."
        jq -s '.[0] * .[1]' "$KUBELET_CONFIG_FILE" <(echo "$KUBELET_CONFIG_CONTENT_JSON") >"$TEMP_CONFIG"
        echo "JSON merge complete"

    elif [[ "$FILE_EXTENSION" =~ ^(yaml|yml)$ ]]; then
        echo "Merging YAML config..."
        yq eval-all 'select(fileIndex==0) * select(fileIndex==1)' \
            "$KUBELET_CONFIG_FILE" <(echo "$KUBELET_CONFIG_CONTENT_YAML") >"$TEMP_CONFIG"
        echo "YAML merge complete"

    else
        echo "WARNING: Unsupported kubelet configuration file type: .$FILE_EXTENSION, skipping kubelet config update" >&2
        rm -f "$TEMP_CONFIG"
        exit 0
    fi

    echo "Moving merged config to $KUBELET_CONFIG_FILE..."
    mv "$TEMP_CONFIG" "$KUBELET_CONFIG_FILE" || {
        echo "Error: Failed to update kubelet config at $KUBELET_CONFIG_FILE" >&2
        exit 0
    }

    echo "Updated kubelet config at $KUBELET_CONFIG_FILE:"
    cat "$KUBELET_CONFIG_FILE"
else
    echo "WARNING: Neither --config-dir nor --config argument found for kubelet; skipping kubelet config update" >&2
    exit 0
fi

# Restart kubelet to apply changes
echo "Attempting to restart kubelet to apply changes..."
success_method=""

if command -v systemctl >/dev/null 2>&1; then
    echo "systemctl is available, trying: systemctl restart kubelet"
    if systemctl restart kubelet; then
        success_method="systemctl"
        echo "kubelet restarted successfully via systemctl"
    else
        # Only run on RKE2 nodes
        if [ -d "/var/lib/rancher/rke2" ]; then
            echo "kubelet systemctl restart failed, attempting rke2-server restart"

            LOCK_FILE="/tmp/rke2-restarted"
            # lock check (prevents restart loop)
            if [ -f "$LOCK_FILE" ]; then
                echo "RKE2 restart already triggered. Skipping."
                exit 0
            fi

            # create lock before restart
            touch "$LOCK_FILE"

            if systemctl restart rke2-server; then
                success_method="systemctl: rke2-server restart"
                echo "rke2-server restarted successfully via systemctl"
            else
                echo "WARNING: systemctl restart rke2-server failed" >&2
            fi
        else
            echo "WARNING: systemctl restart kubelet failed" >&2
        fi
    fi
fi
if [ -z "$success_method" ] && command -v service >/dev/null 2>&1; then
    echo "service is available, trying: service kubelet restart"
    if service kubelet restart; then
        success_method="service"
        echo "kubelet restarted successfully via service"
    else
        echo "WARNING: service kubelet restart failed" >&2
    fi
fi

if [ -z "$success_method" ] && command -v snap >/dev/null 2>&1; then
    echo "snap is available, trying: snap restart kubelet-eks"
    if snap restart kubelet-eks; then
        success_method="snap"
        echo "kubelet restarted successfully via snap"
    else
        echo "WARNING: snap restart kubelet-eks failed" >&2
    fi
fi

if [ -z "$success_method" ]; then
    echo "ERROR: Could not restart kubelet via systemctl, service, or snap; please restart manually" >&2
    exit 1
fi

echo "Done. kubelet configuration update complete (restarted via $success_method)"
