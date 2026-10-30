#!/usr/bin/env bats

# configure-kubelet.sh against a kubelet started with --config, with pidof, ps and systemctl stubbed:
# restarts are recorded, not made.
#
#   bats plugins/k8s/scripts/configure-kubelet.bats

SCRIPT="$BATS_TEST_DIRNAME/configure-kubelet.sh"

setup() {
    TMP="$(mktemp -d)"
    mkdir -p "$TMP/bin"
    export KUBELET_CONFIG="$TMP/config.yaml"
    export ENV=production
    export RESTARTS="$TMP/restarts"

    cat >"$TMP/bin/pidof" <<'EOF'
#!/bin/bash
echo 4242
EOF
    # kubelet's process started "now" (etimes 0), so a config written earlier counts as applied
    cat >"$TMP/bin/ps" <<'EOF'
#!/bin/bash
case "$2" in
args=) echo "/usr/bin/kubelet --config=$KUBELET_CONFIG --v=2" ;;
*) echo 0 ;;
esac
EOF
    cat >"$TMP/bin/systemctl" <<'EOF'
#!/bin/bash
[ "$1" = restart ] && echo "$2" >>"$RESTARTS"
exit 0
EOF
    chmod +x "$TMP/bin/pidof" "$TMP/bin/ps" "$TMP/bin/systemctl"
}

teardown() {
    rm -rf "$TMP"
}

run_script() {
    touch -d '-1 minute' "$KUBELET_CONFIG"
    run env PATH="$TMP/bin:$PATH" bash "$SCRIPT"
}

kubeadm_config() {
    cat >"$KUBELET_CONFIG" <<'EOF'
apiVersion: kubelet.config.k8s.io/v1beta1
authentication:
  anonymous:
    enabled: false
cgroupDriver: systemd
kind: KubeletConfiguration
EOF
    if [ -n "${1:-}" ]; then
        echo "runtimeRequestTimeout: $1" >>"$KUBELET_CONFIG"
    fi
}

@test "adds runtimeRequestTimeout to a YAML config, keeps the rest, and restarts kubelet" {
    kubeadm_config
    cp "$KUBELET_CONFIG" "$TMP/before"
    run_script
    [ "$status" -eq 0 ]
    diff <(cat "$TMP/before"; echo "runtimeRequestTimeout: 10m") "$KUBELET_CONFIG"
    grep -qx kubelet "$RESTARTS"
}

@test "replaces a different runtimeRequestTimeout in place" {
    kubeadm_config 2m
    echo "serializeImagePulls: false" >>"$KUBELET_CONFIG"
    run_script
    [ "$status" -eq 0 ]
    [ "$(grep -c '^runtimeRequestTimeout:' "$KUBELET_CONFIG")" -eq 1 ]
    grep -qx 'runtimeRequestTimeout: 10m' "$KUBELET_CONFIG"
    [ "$(tail -n 1 "$KUBELET_CONFIG")" = "serializeImagePulls: false" ]
    grep -qx kubelet "$RESTARTS"
}

@test "a re-run on the config it wrote changes nothing" {
    kubeadm_config 10m
    cp "$KUBELET_CONFIG" "$TMP/before"
    run_script
    [ "$status" -eq 0 ]
    cmp "$TMP/before" "$KUBELET_CONFIG"
    [ ! -e "$RESTARTS" ]
}
