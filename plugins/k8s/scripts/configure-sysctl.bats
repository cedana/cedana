#!/usr/bin/env bats

# configure-sysctl.sh against a fake /proc/sys file and sysctl.d.
#
#   bats plugins/k8s/scripts/configure-sysctl.bats

SCRIPT="$BATS_TEST_DIRNAME/configure-sysctl.sh"

setup() {
    TMP="$(mktemp -d)"
    export INOTIFY_MAX_USER_INSTANCES_PATH="$TMP/max_user_instances"
    export SYSCTL_CONF_PATH="$TMP/sysctl.d/99-cedana.conf"
}

teardown() {
    rm -rf "$TMP"
}

@test "raises a low limit and persists it" {
    echo 128 >"$INOTIFY_MAX_USER_INSTANCES_PATH"
    run bash "$SCRIPT"
    [ "$status" -eq 0 ]
    [ "$(cat "$INOTIFY_MAX_USER_INSTANCES_PATH")" = 8192 ]
    grep -qx "fs.inotify.max_user_instances = 8192" "$SYSCTL_CONF_PATH"
}

@test "doesn't lower a higher limit, and persists it" {
    echo 65536 >"$INOTIFY_MAX_USER_INSTANCES_PATH"
    run bash "$SCRIPT"
    [ "$status" -eq 0 ]
    [ "$(cat "$INOTIFY_MAX_USER_INSTANCES_PATH")" = 65536 ]
    grep -qx "fs.inotify.max_user_instances = 65536" "$SYSCTL_CONF_PATH"
}

@test "is idempotent: one entry in sysctl.d after reruns" {
    echo 128 >"$INOTIFY_MAX_USER_INSTANCES_PATH"
    bash "$SCRIPT"
    bash "$SCRIPT"
    [ "$(grep -c max_user_instances "$SYSCTL_CONF_PATH")" -eq 1 ]
}

@test "keeps other entries in an existing sysctl.d file" {
    echo 128 >"$INOTIFY_MAX_USER_INSTANCES_PATH"
    mkdir -p "$(dirname "$SYSCTL_CONF_PATH")"
    printf 'vm.max_map_count = 262144\nfs.inotify.max_user_instances = 512\n' >"$SYSCTL_CONF_PATH"
    run bash "$SCRIPT"
    [ "$status" -eq 0 ]
    grep -qx "vm.max_map_count = 262144" "$SYSCTL_CONF_PATH"
    grep -qx "fs.inotify.max_user_instances = 8192" "$SYSCTL_CONF_PATH"
}

@test "honors a custom minimum" {
    echo 128 >"$INOTIFY_MAX_USER_INSTANCES_PATH"
    CEDANA_INOTIFY_MAX_USER_INSTANCES=1024 run bash "$SCRIPT"
    [ "$status" -eq 0 ]
    [ "$(cat "$INOTIFY_MAX_USER_INSTANCES_PATH")" = 1024 ]
}

@test "warns but doesn't fail setup when the limit can't be read" {
    run bash "$SCRIPT"
    [ "$status" -eq 0 ]
    [[ "$output" == *WARNING* ]]
    [ ! -e "$SYSCTL_CONF_PATH" ]
}

@test "warns but doesn't fail setup when the limit can't be raised" {
    [ "$(id -u)" -ne 0 ] || skip "root can write a read-only file"
    echo 128 >"$INOTIFY_MAX_USER_INSTANCES_PATH"
    chmod 444 "$INOTIFY_MAX_USER_INSTANCES_PATH"
    run bash "$SCRIPT"
    [ "$status" -eq 0 ]
    [[ "$output" == *"failed to raise"* ]]
    [ ! -e "$SYSCTL_CONF_PATH" ]
}
