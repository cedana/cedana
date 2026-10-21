#!/usr/bin/env bats

# configure-containerd.sh against fixture configs in a temp dir, with systemctl and ps stubbed:
# restarts are recorded, not made.
#
#   bats plugins/k8s/scripts/configure-containerd.bats
#
# containerd itself (if installed) checks that the result parses: a config it cannot load keeps it
# from starting, which takes the node NotReady.

SCRIPT="$BATS_TEST_DIRNAME/configure-containerd.sh"

setup() {
    TMP="$(mktemp -d)"
    mkdir -p "$TMP/bin" "$TMP/etc/containerd"
    export CONTAINERD_CONFIG_PATH="$TMP/etc/containerd/config.toml"
    export CEDANA_PLUGINS_BIN_DIR="/usr/local/bin"
    export ENV=production
    export RESTARTS="$TMP/restarts"

    # containerd's process started "now" (etimes 0), so a config written earlier counts as
    # applied: only a change made by the script itself should restart it
    cat >"$TMP/bin/systemctl" <<'EOF'
#!/bin/bash
case "$1" in
restart) echo "$2" >>"$RESTARTS" ;;
show) echo 4242 ;;
esac
exit 0
EOF
    cat >"$TMP/bin/ps" <<'EOF'
#!/bin/bash
echo 0
EOF
    chmod +x "$TMP/bin/systemctl" "$TMP/bin/ps"
}

teardown() {
    rm -rf "$TMP"
}

run_script() {
    # A config last written a minute ago: older than containerd's (stubbed) start
    touch -d '-1 minute' "$CONTAINERD_CONFIG_PATH"
    run env PATH="$TMP/bin:$PATH" bash "$SCRIPT"
}

cedana_blocks() {
    grep -cE "runtimes\\.([\"']?)cedana\\1\\]" "$CONTAINERD_CONFIG_PATH" || true
}

assert_containerd_loads() {
    if ! command -v containerd >/dev/null; then
        echo "containerd not installed: not checking that the config loads" >&2
        return 0
    fi
    run containerd --config "$CONTAINERD_CONFIG_PATH" config dump
    [ "$status" -eq 0 ] || {
        echo "$output" >&2
        return 1
    }
}

v2_config_with_cedana() {
    local key="$1"
    cat >"$CONTAINERD_CONFIG_PATH" <<EOF
version = 2

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
  runtime_type = "io.containerd.runc.v2"

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.$key]
  runtime_type = "io.containerd.runc.v2"
  runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
EOF
}

@test "first install adds one cedana runtime and restarts containerd" {
    cat >"$CONTAINERD_CONFIG_PATH" <<'EOF'
version = 2

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
  runtime_type = "io.containerd.runc.v2"
EOF
    run_script
    [ "$status" -eq 0 ]
    [ "$(cedana_blocks)" -eq 1 ]
    grep -qx containerd "$RESTARTS"
    assert_containerd_loads
}

@test "a re-run on the config it wrote changes nothing" {
    v2_config_with_cedana '"cedana"'
    run_script
    [ "$status" -eq 0 ]
    [ "$(cedana_blocks)" -eq 1 ]
    [ ! -e "$RESTARTS" ]
    assert_containerd_loads
}

# What took two 8xb200 instances to NotReady on 2026-10-20: a tool that re-serializes
# config.toml writes the key unquoted, the helper's next
# setup does not recognize it, appends a second block, and containerd refuses the duplicate table.
@test "a re-run on a config rewritten with the key unquoted does not add a second runtime" {
    v2_config_with_cedana 'cedana'
    run_script
    [ "$status" -eq 0 ]
    assert_containerd_loads
    [ "$(cedana_blocks)" -eq 1 ]
    [ ! -e "$RESTARTS" ]
}

@test "a re-run on a config rewritten with the key single-quoted does not add a second runtime" {
    v2_config_with_cedana "'cedana'"
    run_script
    [ "$status" -eq 0 ]
    assert_containerd_loads
    [ "$(cedana_blocks)" -eq 1 ]
    [ ! -e "$RESTARTS" ]
}
