#!/bin/bash
set -euo pipefail

# Registers the cedana runtime with containerd (or k3s/RKE2's containerd template) and restarts the
# service when the configuration changed or has not been applied yet. Run by the k8s helper's setup
# after install-plugins.sh; tested by configure-containerd.bats.

if [ "$ENV" != "production" ]; then
    echo "Non-production environment detected, skipping containerd runtime configuration" >&2
    exit 0
fi

echo "Starting containerd runtime configuration..."

# The CRI plugin config key differs between containerd config version 2 and 3
cedana_runtime_config() {
    if [ "$1" = "3" ]; then
        cat <<END_CAT
[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes."cedana"]
  runtime_type = "io.containerd.runc.v2"
  runtime_path = "${CEDANA_PLUGINS_BIN_DIR}/cedana-shim-runc-v2"
END_CAT
    else
        # containerd 1.7 drops its built-in runc runtime once a config defines any runtime, so
        # with none defined yet, define it too ("with-runc"): otherwise the default runtime is
        # gone, and the CRI plugin fails to load
        if [ "${2:-}" = "with-runc" ]; then
            cat <<END_CAT
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes."runc"]
  runtime_type = "io.containerd.runc.v2"

END_CAT
        fi
        cat <<END_CAT
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes."cedana"]
  runtime_type = "io.containerd.runc.v2"
  runtime_path = "${CEDANA_PLUGINS_BIN_DIR}/cedana-shim-runc-v2"
END_CAT
    fi
}

has_cedana_runtime() {
    grep -Eqs "runtimes\.(cedana|\"cedana\"|'cedana')\]" "$1"
}

CONFIG_CHANGED=false

# Every file the script changes is backed up first, so that a config containerd refuses (or that
# fails the restart) is rolled back instead of leaving the node without a container runtime.
BACKUP_DIR=$(mktemp -d)
BACKUP_PATHS=()
trap 'rm -rf "$BACKUP_DIR"' EXIT

backup() {
    if [ -e "$1" ]; then
        cp -p "$1" "$BACKUP_DIR/${#BACKUP_PATHS[@]}"
    fi
    BACKUP_PATHS+=("$1")
}

rollback() {
    local i
    for i in "${!BACKUP_PATHS[@]}"; do
        if [ -e "$BACKUP_DIR/$i" ]; then
            cp -p "$BACKUP_DIR/$i" "${BACKUP_PATHS[$i]}"
        else
            rm -f "${BACKUP_PATHS[$i]}"
        fi
        echo "Rolled back ${BACKUP_PATHS[$i]}" >&2
    done
}

# k3s and RKE2 manage containerd themselves: <data-dir>/agent/etc/containerd/config.toml
# is regenerated on every service start, so editing it directly is not persistent.
# Instead, both support extending the generated config through a template that
# includes the built-in "base" template: config.toml.tmpl (config version 2) or
# config-v3.toml.tmpl (config version 3, containerd 2.0+, k3s/RKE2 v1.31.6+/v1.32.2+).
#
# The generated config.toml only exists if k3s/RKE2 actually manages containerd;
# with an external containerd (--container-runtime-endpoint) it is never
# generated and templates are never rendered, so fall through to the regular
# containerd flow below in that case.
# Resolve a k3s/RKE2 data dir: --data-dir flag of the running process, then
# config.yaml, then the default location
rancher_data_dir() {
    local name="$1" pid args dir=""
    pid=$(pidof -s "$name" 2>/dev/null || true)
    if [ -n "$pid" ]; then
        args=$(ps -o args= -p "$pid" 2>/dev/null || true)
        dir=$(echo "$args" | sed -n 's/.*--data-dir[= ]\+\([^ ]*\).*/\1/p')
    fi
    if [ -z "$dir" ] && [ -f "/etc/rancher/$name/config.yaml" ]; then
        dir=$(sed -n 's/^[[:space:]]*data-dir:[[:space:]]*//p' "/etc/rancher/$name/config.yaml" | head -n 1 | tr -d '"' | tr -d "'")
    fi
    echo "${dir:-/var/lib/rancher/$name}"
}

RANCHER_SERVICES=""
for RANCHER_NAME in rke2 k3s; do
    RANCHER_DATA_DIR=$(rancher_data_dir "$RANCHER_NAME")
    if [ -f "$RANCHER_DATA_DIR/agent/etc/containerd/config.toml" ]; then
        echo "$RANCHER_NAME node detected (with managed containerd, data dir: $RANCHER_DATA_DIR)"
        RANCHER_CONFIG_DIR="$RANCHER_DATA_DIR/agent/etc/containerd"
        if [ "$RANCHER_NAME" = "rke2" ]; then
            RANCHER_SERVICES="rke2-server rke2-agent"
        else
            RANCHER_SERVICES="k3s k3s-agent"
        fi
        break
    fi
done

if [ -n "$RANCHER_SERVICES" ]; then
    mkdir -p "$RANCHER_CONFIG_DIR"

    # An existing template takes precedence (config-v3.toml.tmpl wins over
    # config.toml.tmpl); otherwise match the version of the generated config.
    if [ -f "$RANCHER_CONFIG_DIR/config-v3.toml.tmpl" ]; then
        TEMPLATE="$RANCHER_CONFIG_DIR/config-v3.toml.tmpl"
        CONTAINERD_VERSION=3
    elif [ -f "$RANCHER_CONFIG_DIR/config.toml.tmpl" ]; then
        TEMPLATE="$RANCHER_CONFIG_DIR/config.toml.tmpl"
        CONTAINERD_VERSION=2
    elif grep -qs 'version = 3' "$RANCHER_CONFIG_DIR/config.toml"; then
        TEMPLATE="$RANCHER_CONFIG_DIR/config-v3.toml.tmpl"
        CONTAINERD_VERSION=3
    else
        TEMPLATE="$RANCHER_CONFIG_DIR/config.toml.tmpl"
        CONTAINERD_VERSION=2
    fi
    echo "Using containerd config template $TEMPLATE (config version $CONTAINERD_VERSION)"

    if ! has_cedana_runtime "$TEMPLATE"; then
        echo "Adding cedana runtime config to $TEMPLATE"
        backup "$TEMPLATE"
        if [ ! -f "$TEMPLATE" ]; then
            echo '{{ template "base" . }}' >"$TEMPLATE"
        fi
        {
            echo ""
            cedana_runtime_config "$CONTAINERD_VERSION"
        } >>"$TEMPLATE"
        CONFIG_CHANGED=true
    else
        echo "Cedana runtime config already exists in $TEMPLATE, skipping"
    fi

    RESTART_STAMP_FILES="$TEMPLATE"
else
    CONTAINERD_CONFIG_PATH=${CONTAINERD_CONFIG_PATH:-"/etc/containerd/config.toml"}
    CONTAINERD_CONFD_DIR="$(dirname "$CONTAINERD_CONFIG_PATH")/conf.d"
    echo "Using containerd config path: $CONTAINERD_CONFIG_PATH"

    if [ ! -f "$CONTAINERD_CONFIG_PATH" ]; then
        echo "ERROR: containerd config file not found at $CONTAINERD_CONFIG_PATH" >&2
        exit 1
    fi

    # Detect containerd config version. containerd migrates every file (main
    # config and each import) from its own version, and a file without a
    # version line is version 1, so the main config alone is not enough: use
    # the highest version across the main config and conf.d files. Version 3+
    # files only load on containerd 2.x.
    CONTAINERD_VERSION=0
    for FILE in "$CONTAINERD_CONFIG_PATH" "$CONTAINERD_CONFD_DIR"/*.toml; do
        [ -f "$FILE" ] && [ "$FILE" != "$CONTAINERD_CONFD_DIR/999-cedana.toml" ] || continue
        FILE_VERSION=$(sed -n 's/^[[:space:]]*version[[:space:]]*=[[:space:]]*\([0-9]\+\).*/\1/p' "$FILE" | head -n 1)
        if [ "${FILE_VERSION:-0}" -gt "$CONTAINERD_VERSION" ]; then
            CONTAINERD_VERSION=$FILE_VERSION
        fi
    done
    if [ "$CONTAINERD_VERSION" -lt 2 ]; then
        echo "ERROR: Unsupported containerd config version. Version 2 or higher is required." >&2
        exit 1
    fi
    echo "Detected containerd config version $CONTAINERD_VERSION"

    if [ "$CONTAINERD_VERSION" = "2" ]; then
        # Whether any config defines runtimes (see cedana_runtime_config), in any form: a runtimes
        # table ([...containerd.runtimes]), a runtime's ([...containerd.runtimes.runc]), or a key
        # (runtimes = {...}, runtimes.runc = {...}). Adding runc where it's already defined, e.g. as
        # an inline table, redefines it, and containerd refuses the config.
        RUNC_ARG=with-runc
        for FILE in "$CONTAINERD_CONFIG_PATH" "$CONTAINERD_CONFD_DIR"/*.toml; do
            [ -f "$FILE" ] && [ "$FILE" != "$CONTAINERD_CONFD_DIR/999-cedana.toml" ] || continue
            if grep -qE '^[[:space:]]*(\[[^]]*containerd\.runtimes(\]|\.)|runtimes[[:space:]]*(=|\.))' "$FILE"; then
                RUNC_ARG=""
                break
            fi
        done

        # Version 2: conf.d imports cannot merge multiple runtime tables
        # (https://github.com/containerd/containerd/issues/5837, fixed in v3),
        # so copy the last conf.d file (lexicographically) and append the
        # cedana runtime to the copy.
        LAST_CONFD_FILE=""
        if [ -d "$CONTAINERD_CONFD_DIR" ]; then
            LAST_CONFD_FILE=$(find "$CONTAINERD_CONFD_DIR" -maxdepth 1 -type f -name "*.toml" ! -name "999-cedana.toml" 2>/dev/null | sort | tail -n 1)
        fi

        if [ -n "$LAST_CONFD_FILE" ]; then
            TARGET_CONFIG="$CONTAINERD_CONFD_DIR/999-cedana.toml"
            echo "Basing cedana runtime config on last existing conf.d file: $LAST_CONFD_FILE"
            # After a reboot, the NVIDIA toolkit rewrites its conf.d file from the merged config,
            # which has the cedana runtime by then: adding it again defines it twice
            NEW_CONFIG=$(
                cat "$LAST_CONFD_FILE"
                if ! has_cedana_runtime "$LAST_CONFD_FILE"; then
                    echo ""
                    cedana_runtime_config 2 "$RUNC_ARG"
                fi
            )
            if [ "$NEW_CONFIG" != "$(cat "$TARGET_CONFIG" 2>/dev/null)" ]; then
                echo "Writing cedana runtime config to $TARGET_CONFIG"
                backup "$TARGET_CONFIG"
                echo "$NEW_CONFIG" >"$TARGET_CONFIG"
                CONFIG_CHANGED=true
            else
                echo "Cedana runtime config already up to date in $TARGET_CONFIG, skipping"
            fi
        else
            # Directly add to main config if no conf.d files exist, so that
            # when the NVIDIA plugin is added later it can copy from this and
            # not miss the cedana config.
            TARGET_CONFIG="$CONTAINERD_CONFIG_PATH"
            if ! has_cedana_runtime "$TARGET_CONFIG"; then
                echo "No conf.d files found, adding cedana runtime config to $TARGET_CONFIG"
                backup "$TARGET_CONFIG"
                {
                    echo ""
                    cedana_runtime_config 2 "$RUNC_ARG"
                } >>"$TARGET_CONFIG"
                CONFIG_CHANGED=true
            else
                echo "Cedana runtime config already exists in $TARGET_CONFIG, skipping"
            fi
        fi
    else
        # Version 3+ (containerd 2.x): conf.d files merge properly, so ship a
        # dedicated conf.d file with only the cedana runtime and make sure it
        # gets imported. The file has no version line, as containerd rejects
        # drop-ins with a higher version than the main config.
        TARGET_CONFIG="$CONTAINERD_CONFD_DIR/999-cedana.toml"
        mkdir -p "$CONTAINERD_CONFD_DIR"

        if ! grep -qF "$CONTAINERD_CONFD_DIR/*.toml" "$CONTAINERD_CONFIG_PATH"; then
            backup "$CONTAINERD_CONFIG_PATH"
            if grep -q '^imports = \[.*\]' "$CONTAINERD_CONFIG_PATH"; then
                echo "Appending conf.d glob to existing imports in $CONTAINERD_CONFIG_PATH"
                sed -i "s|^imports = \[\(.*\)\]|imports = [\1, \"$CONTAINERD_CONFD_DIR/*.toml\"]|" "$CONTAINERD_CONFIG_PATH"
            elif grep -q '^imports = \[' "$CONTAINERD_CONFIG_PATH"; then
                # Multiline imports array: add the glob right after the opening
                # bracket (TOML allows a trailing comma before the closing one)
                echo "Adding conf.d glob to multiline imports in $CONTAINERD_CONFIG_PATH"
                sed -i "/^imports = \[/a \"$CONTAINERD_CONFD_DIR/*.toml\"," "$CONTAINERD_CONFIG_PATH"
            elif grep -q '^[[:space:]]*version[[:space:]]*=' "$CONTAINERD_CONFIG_PATH"; then
                echo "Adding imports line to $CONTAINERD_CONFIG_PATH"
                sed -i "/^[[:space:]]*version[[:space:]]*=/a imports = [\"$CONTAINERD_CONFD_DIR/*.toml\"]" "$CONTAINERD_CONFIG_PATH"
            else
                # No version line: top-level keys must precede all tables
                echo "Adding imports line to $CONTAINERD_CONFIG_PATH"
                sed -i "1i imports = [\"$CONTAINERD_CONFD_DIR/*.toml\"]" "$CONTAINERD_CONFIG_PATH"
            fi
            if ! grep -qF "$CONTAINERD_CONFD_DIR/*.toml" "$CONTAINERD_CONFIG_PATH"; then
                echo "ERROR: Failed to add $CONTAINERD_CONFD_DIR/*.toml to imports in $CONTAINERD_CONFIG_PATH, please add it manually" >&2
                rollback
                exit 1
            fi
            CONFIG_CHANGED=true
        else
            echo "conf.d glob already present in imports, no changes needed"
        fi

        if ! has_cedana_runtime "$TARGET_CONFIG"; then
            echo "Creating cedana runtime config at $TARGET_CONFIG"
            backup "$TARGET_CONFIG"
            cedana_runtime_config 3 >"$TARGET_CONFIG"
            CONFIG_CHANGED=true
        else
            echo "Cedana runtime config already exists in $TARGET_CONFIG, skipping"
        fi
    fi

    RESTART_STAMP_FILES="$TARGET_CONFIG $CONTAINERD_CONFIG_PATH"
fi

# Determine which service provides containerd on this node
RESTART_SERVICE="containerd"
if [ -n "$RANCHER_SERVICES" ]; then
    RESTART_SERVICE=""
    for SERVICE in $RANCHER_SERVICES; do
        if systemctl is-active --quiet "$SERVICE"; then
            RESTART_SERVICE="$SERVICE"
            break
        fi
    done
    if [ -z "$RESTART_SERVICE" ]; then
        echo "WARNING: No active service found among: $RANCHER_SERVICES; restart it manually to apply the containerd configuration" >&2
        exit 0
    fi
fi

# Epoch time the service's main process started (0 if unknown)
service_started_at() {
    local pid elapsed
    pid=$(systemctl show -p MainPID --value "$1" 2>/dev/null || true)
    if [ -z "$pid" ] || [ "$pid" = "0" ]; then
        echo 0
        return
    fi
    elapsed=$(ps -o etimes= -p "$pid" 2>/dev/null | head -n 1 | tr -d ' ')
    if [ -z "$elapsed" ]; then
        echo 0
        return
    fi
    echo $(($(date +%s) - elapsed))
}

# Even when the config is already up to date, restart if the service hasn't
# started since the config was last written (e.g. a previous run wrote the
# config but its restart failed)
if [ "$CONFIG_CHANGED" = false ]; then
    STARTED_AT=$(service_started_at "$RESTART_SERVICE")
    NEWEST_MTIME=0
    for FILE in $RESTART_STAMP_FILES; do
        [ -f "$FILE" ] || continue
        MTIME=$(stat -c %Y "$FILE")
        if [ "$MTIME" -gt "$NEWEST_MTIME" ]; then
            NEWEST_MTIME=$MTIME
        fi
    done
    if [ "$STARTED_AT" -eq 0 ] || [ "$STARTED_AT" -ge "$NEWEST_MTIME" ]; then
        echo "Containerd runtime configuration already up to date, no restart needed"
        exit 0
    fi
    echo "Containerd runtime configuration up to date but not applied ($RESTART_SERVICE started before it was written)"
fi

# Load the config (main file and every import) the way containerd will on restart, without
# restarting it. k3s/RKE2 render their template only on start, so there's nothing to load yet.
if [ -z "$RANCHER_SERVICES" ]; then
    CONTAINERD_BIN_PATH=$(command -v "${CONTAINERD_BIN:-containerd}" || true)
    if [ -z "$CONTAINERD_BIN_PATH" ]; then
        echo "WARNING: containerd binary not found, not validating $CONTAINERD_CONFIG_PATH before restarting" >&2
    else
        echo "Validating $CONTAINERD_CONFIG_PATH with $CONTAINERD_BIN_PATH"
        if ! VALIDATE_OUTPUT=$("$CONTAINERD_BIN_PATH" --config "$CONTAINERD_CONFIG_PATH" config dump 2>&1 >/dev/null); then
            echo "$VALIDATE_OUTPUT" >&2
            echo "ERROR: containerd does not load $CONTAINERD_CONFIG_PATH, rolling back this run's changes; not restarting $RESTART_SERVICE" >&2
            rollback
            exit 1
        fi
    fi
fi

# k3s/RKE2 only re-render config.toml from the template on service restart;
# no restart here disrupts running containers
echo "Restarting $RESTART_SERVICE to pick up the cedana runtime configuration..."
if ! systemctl restart "$RESTART_SERVICE"; then
    if [ "${#BACKUP_PATHS[@]}" -eq 0 ]; then
        echo "ERROR: Failed to restart $RESTART_SERVICE, please restart manually" >&2
        exit 1
    fi
    echo "ERROR: Failed to restart $RESTART_SERVICE with the cedana runtime configuration, rolling it back" >&2
    rollback
    if ! systemctl restart "$RESTART_SERVICE"; then
        echo "ERROR: Failed to restart $RESTART_SERVICE after rolling back, please restart manually" >&2
    fi
    exit 1
fi
echo "Restarted $RESTART_SERVICE successfully"
