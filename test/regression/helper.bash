#!/usr/bin/env bash

# Helper functions that hit the local Cedana API

function exec_task() {
    local task="$1"
    local job_id="$2"
    cedana exec -w "$PWD" "$task" -i "$job_id"
}

function checkpoint_task() {
    local job_id="$1"
    cedana dump job "$job_id" -d /tmp
}

function restore_task() {
    local job_id="$1"
    cedana restore job "$job_id"
}

function start_busybox(){
  local container_name="$1"

  sudo ctr image pull docker.io/library/busybox:latest

  sudo ctr run -d docker.io/library/busybox:latest "$container_name" sh -c 'while true; do sleep 3600; done'
}

function rootfs_checkpoint() {
  local container_id="$1"
  local image_ref="$2"
  local containerd_sock="$3"
  local namespace="$4"

  cedana dump rootfs -p "$container_id" --ref "$image_ref" -a "$containerd_sock" -n "$namespace"
}

function rootfs_restore() {
  local container_id="$1"
  local image_ref="$2"
  local containerd_sock="$3"
  local namespace="$4"

  cedana restore rootfs -p "$container_id" --ref "$image_ref" -a "$containerd_sock" -n "$namespace"
}

function runc_checkpoint() {
  local dir="$1"
  local job_id="$2"
  cedana dump runc --dir "$dir" --id "$job_id"
}

function runc_restore() {
  local bundle="$1"
  local dir="$2"
  local id="$3"
  local tty="$4"
  cedana restore runc -e -b "$bundle" --dir "$dir" --id "$id" --console-socket "$tty"
}

function has_buildah() {
    if [ ! -e "$(command -v buildah)" ]; then
        skip "buildah binary not found"
    fi
}

function run_buildah() {
    buildah --log-level debug --root "$TESTDIR/crio" "$@"
}

function crio_rootfs_checkpoint() {
  local container_storage="$1"
  local destination="$2"
  local container_id="$3"

  cedana dump CRIORootfs -s "$container_storage" -i "$container_id" -d "$destination"
}

function setup_crio() {
    apparmor=""
    if [[ -n "$1" ]]; then
        apparmor="$1"
    fi

    for img in "${IMAGES[@]}"; do
        setup_img "$img"
    done

    # Prepare the CNI configuration files, we're running with non host
    # networking by default
    CNI_DEFAULT_NETWORK=${CNI_DEFAULT_NETWORK:-crio}
    CNI_TYPE=${CNI_TYPE:-bridge}

    RUNTIME_ROOT=${RUNTIME_ROOT:-"$TESTDIR/crio-runtime-root"}
    # export here so direct calls to crio later inherit the variable
    export CONTAINER_RUNTIMES=${CONTAINER_RUNTIMES:-$CONTAINER_DEFAULT_RUNTIME:$RUNTIME_BINARY_PATH:$RUNTIME_ROOT:$RUNTIME_TYPE:$PRIVILEGED_WITHOUT_HOST_DEVICES:$RUNTIME_CONFIG_PATH}

    # generate the default config file
    "$CRIO_BINARY_PATH" config --default >"$CRIO_CONFIG"

    # shellcheck disable=SC2086
    "$CRIO_BINARY_PATH" \
        --hooks-dir="$HOOKSDIR" \
        --apparmor-profile "$apparmor" \
        --cgroup-manager "$CONTAINER_CGROUP_MANAGER" \
        --conmon "$CONMON_BINARY" \
        --container-attach-socket-dir "$CONTAINER_ATTACH_SOCKET_DIR" \
        --container-exits-dir "$CONTAINER_EXITS_DIR" \
        --listen "$CRIO_SOCKET" \
        --irqbalance-config-file "$IRQBALANCE_CONFIG_FILE" \
        --irqbalance-config-restore-file "$IRQBALANCE_CONFIG_RESTORE_FILE" \
        --signature-policy "$SIGNATURE_POLICY" \
        --signature-policy-dir "$SIGNATURE_POLICY_DIR" \
        -r "$TESTDIR/crio" \
        --runroot "$TESTDIR/crio-run" \
        --cni-default-network "$CNI_DEFAULT_NETWORK" \
        --cni-config-dir "$CRIO_CNI_CONFIG" \
        --cni-plugin-dir "$CRIO_CNI_PLUGIN" \
        --pinns-path "$PINNS_BINARY_PATH" \
        $STORAGE_OPTIONS \
        -c "" \
        -d "" \
        $OVERRIDE_OPTIONS \
        config >"$CRIO_CUSTOM_CONFIG"
    # make sure we don't run with nodev, or else mounting a readonly rootfs will fail: https://github.com/cri-o/cri-o/issues/1929#issuecomment-474240498
    sed -r -e 's/nodev(,)?//g' -i "$CRIO_CONFIG"
    sed -r -e 's/nodev(,)?//g' -i "$CRIO_CUSTOM_CONFIG"
    prepare_network_conf
}


function start_crio_no_setup() {
    "$CRIO_BINARY_PATH" \
        --default-mounts-file "$TESTDIR/containers/mounts.conf" \
        -l debug \
        -c "$CRIO_CONFIG" \
        -d "$CRIO_CONFIG_DIR" \
        &>"$CRIO_LOG" &
    CRIO_PID=$!
    wait_until_reachable
}

function check_images() {
    local img json list

    # check that images are there
    json=$(crictl images -o json)
    [ -n "$json" ]
    list=$(jq -r '.images[] | .repoTags[]' <<<"$json")
    for img in "${IMAGES[@]}"; do
        if [[ "$list" != *"$img"* ]]; then
            echo "Image $img is not present but it should!" >&2
            exit 1
        fi
    done

    # these two variables are used by a few tests
    eval "$(jq -r '.images[] |
        select(.repoTags[0] == "quay.io/crio/fedora-crio-ci:latest") |
        "REDIS_IMAGEID=" + .id + "\n" +
	"REDIS_IMAGEREF=" + .repoDigests[0]' <<<"$json")"
}
