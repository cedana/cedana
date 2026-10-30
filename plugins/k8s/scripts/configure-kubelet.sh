#!/bin/bash
set -euo pipefail

if [ "$ENV" != "production" ]; then
    echo "Running in non-production environment; skipping kubelet configuration update" >&2
    exit 0
fi

# Only merging into a --config file needs jq (JSON) or yq (YAML); a drop-in config dir needs neither
require_tool() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "Error: '$1' is not installed. Please install it to update $2 kubelet configurations." >&2
        exit 1
    fi
    echo "$1 found at: $(command -v "$1") ($("$1" --version))"
}

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
K3S_UNIT=""
if [ -z "$KUBELET_PID" ]; then
    # k3s runs kubelet inside its own process, so look for the k3s service instead
    if command -v systemctl >/dev/null 2>&1; then
        for unit in k3s k3s-agent; do
            if systemctl is-active --quiet "$unit"; then
                K3S_UNIT="$unit"
                break
            fi
        done
    fi
    if [ -z "$K3S_UNIT" ]; then
        echo "Error: kubelet is not running" >&2
        exit 1
    fi
    KUBELET_PID=$(systemctl show -p MainPID --value "$K3S_UNIT")
    echo "Found k3s service $K3S_UNIT running kubelet (PID=$KUBELET_PID)"
else
    echo "Found kubelet process (PID=$KUBELET_PID)"
fi

# True if kubelet (re)started after the given config file was last written,
# i.e. the config is already in effect (guards against a previous run that
# wrote the config but failed to restart kubelet)
kubelet_has_config() {
    local elapsed started
    elapsed=$(ps -o etimes= -p "$KUBELET_PID" 2>/dev/null | head -n 1 | tr -d ' ')
    [ -n "$elapsed" ] || return 1
    started=$(($(date +%s) - elapsed))
    [ "$started" -ge "$(stat -c %Y "$1")" ]
}

if [ -n "$K3S_UNIT" ]; then
    # k3s passes its kubelet a drop-in config dir (v1.32+)
    KUBELET_CONFIG_DIR="/var/lib/rancher/k3s/agent/etc/kubelet.conf.d"
    KUBELET_CONFIG_FILE=""
    if [ ! -d "$KUBELET_CONFIG_DIR" ]; then
        echo "WARNING: k3s kubelet config dir $KUBELET_CONFIG_DIR not found (requires k3s v1.32+); skipping kubelet config update, please manually set runtimeRequestTimeout=$KUBELET_RUNTIME_REQUEST_TIMEOUT" >&2
        exit 0
    fi
else
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
fi

echo "Resolved --config-dir: ${KUBELET_CONFIG_DIR:-<not set>}"
echo "Resolved --config:     ${KUBELET_CONFIG_FILE:-<not set>}"

if [ -n "$KUBELET_CONFIG_DIR" ]; then
    TARGET="$KUBELET_CONFIG_DIR/99-cedana.conf"
    echo "Strategy: drop-in config dir, writing to $TARGET"

    if [ "$(cat "$TARGET" 2>/dev/null)" = "$KUBELET_CONFIG_CONTENT_JSON" ] && kubelet_has_config "$TARGET"; then
        echo "Kubelet config already up to date at $TARGET, no restart needed"
        exit 0
    fi

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
    case "$FILE_EXTENSION" in
        json) require_tool jq JSON ;;
        yaml | yml) require_tool yq YAML ;;
    esac
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

    if cmp -s "$TEMP_CONFIG" "$KUBELET_CONFIG_FILE" && kubelet_has_config "$KUBELET_CONFIG_FILE"; then
        echo "Kubelet config already up to date at $KUBELET_CONFIG_FILE, no restart needed"
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

if [ -n "$K3S_UNIT" ]; then
    # Restarting k3s does not stop running containers
    echo "Restarting k3s service $K3S_UNIT"
    if systemctl restart "$K3S_UNIT"; then
        echo "Done. kubelet configuration update complete (restarted $K3S_UNIT)"
        exit 0
    fi
    echo "ERROR: systemctl restart $K3S_UNIT failed; please restart manually" >&2
    exit 1
fi

if command -v systemctl >/dev/null 2>&1; then
    echo "systemctl is available, trying: systemctl restart kubelet"
    if systemctl restart kubelet; then
        success_method="systemctl"
        echo "kubelet restarted successfully via systemctl"
    else
        # Only run on RKE2 nodes
        if [ -d "/var/lib/rancher/rke2" ]; then
            echo "kubelet systemctl restart failed, attempting rke2-server restart"

            LOCK_FILE="/tmp/cedana-rke2-restarted"
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
