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

nvidia_v2_confd() {
    mkdir -p "$TMP/etc/containerd/conf.d"
    cat >"$CONTAINERD_CONFIG_PATH" <<EOF2
version = 2
imports = ["$TMP/etc/containerd/conf.d/*.toml"]
EOF2
    cat >"$TMP/etc/containerd/conf.d/99-nvidia.toml" <<'EOF2'
version = 2

[plugins]

  [plugins."io.containerd.grpc.v1.cri"]

    [plugins."io.containerd.grpc.v1.cri".containerd]
      default_runtime_name = "nvidia"

      [plugins."io.containerd.grpc.v1.cri".containerd.runtimes]

        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.nvidia]
          runtime_type = "io.containerd.runc.v2"

          [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.nvidia.options]
            BinaryName = "/usr/bin/nvidia-container-runtime"
EOF2
}

confd_cedana_blocks() {
    grep -cE "runtimes\\.([\"']?)cedana\\1\\]" "$TMP/etc/containerd/conf.d/999-cedana.toml" || true
}

# After a reboot, the NVIDIA toolkit rewrites 99-nvidia.toml from the merged config, which by
# then has the cedana runtime from 999-cedana.toml: copying it and appending the runtime again
# defined it twice ("toml: table cedana already exists"), and containerd didn't come back.
@test "a re-run after the NVIDIA toolkit rewrote its conf.d file with the cedana runtime does not define it twice" {
    nvidia_v2_confd
    run_script
    [ "$status" -eq 0 ]
    [ "$(confd_cedana_blocks)" -eq 1 ]

    cat >>"$TMP/etc/containerd/conf.d/99-nvidia.toml" <<'EOF2'

        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.cedana]
          runtime_path = "/usr/local/bin/cedana-shim-runc-v2"
          runtime_type = "io.containerd.runc.v2"
EOF2
    rm -f "$RESTARTS"
    run_script
    [ "$status" -eq 0 ]
    [ "$(confd_cedana_blocks)" -eq 1 ]
    cmp "$TMP/etc/containerd/conf.d/99-nvidia.toml" "$TMP/etc/containerd/conf.d/999-cedana.toml"
    grep -qx containerd "$RESTARTS"
    assert_containerd_loads

    rm -f "$RESTARTS"
    run_script
    [ "$status" -eq 0 ]
    [ ! -e "$RESTARTS" ]
}

@test "a config containerd refuses is rolled back and containerd is not restarted" {
    nvidia_v2_confd
    printf '#!/bin/sh\necho "toml: table cedana already exists" >&2\nexit 1\n' >"$TMP/bin/containerd-refuses"
    chmod +x "$TMP/bin/containerd-refuses"
    echo "previous" >"$TMP/etc/containerd/conf.d/999-cedana.toml"
    export CONTAINERD_BIN="$TMP/bin/containerd-refuses"
    run_script
    [ "$status" -eq 1 ]
    [[ "$output" == *"rolling it back"* ]]
    [ "$(cat "$TMP/etc/containerd/conf.d/999-cedana.toml")" = "previous" ]
    [ ! -e "$RESTARTS" ]
}

@test "a config written by this run is removed when containerd refuses it" {
    nvidia_v2_confd
    printf '#!/bin/sh\nexit 1\n' >"$TMP/bin/containerd-refuses"
    chmod +x "$TMP/bin/containerd-refuses"
    export CONTAINERD_BIN="$TMP/bin/containerd-refuses"
    run_script
    [ "$status" -eq 1 ]
    [ ! -e "$TMP/etc/containerd/conf.d/999-cedana.toml" ]
    [ ! -e "$RESTARTS" ]
}

@test "a failed restart rolls the config back and restarts containerd again" {
    nvidia_v2_confd
    echo "previous" >"$TMP/etc/containerd/conf.d/999-cedana.toml"
    cat >"$TMP/bin/systemctl" <<'EOF2'
#!/bin/bash
case "$1" in
restart)
    echo "$2" >>"$RESTARTS"
    [ "$(wc -l <"$RESTARTS")" -gt 1 ]
    exit
    ;;
show) echo 4242 ;;
esac
exit 0
EOF2
    export CONTAINERD_BIN=true
    run_script
    [ "$status" -eq 1 ]
    [ "$(cat "$TMP/etc/containerd/conf.d/999-cedana.toml")" = "previous" ]
    [ "$(grep -c containerd "$RESTARTS")" -eq 2 ]
}
