#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes

load ../helpers/utils
load ../helpers/daemon
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

setup_file() {
    # This test only applies when the pod rootfs is an overlay mount (the only
    # case with an RW/upper layer to dump + restore). On K3s CI the pods run in a
    # nested privileged container whose rootfs is not an overlay mount point
    if [ "$(echo "${PROVIDER:-}" | tr '[:upper:]' '[:lower:]')" = "k3s" ]; then
        skip "RW-layer overlay dump/restore requires an overlay-mounted rootfs (not available on K3s CI)"
    fi
}

# Regression test for the overlay RW-layer restore coherence bug (CED-2220).
#
# The bug: on restore the container's RW (upper) layer must be repopulated into
# the fresh snapshot's upperdir.
#
# Previously that write happened after the shim had already mounted the overlay
# (post-mount, in the runc CRIU pre-restore callback).
# overlayfs does not guarantee that a file created in the upperdir of
# an already-mounted overlay is visible through the merged mount, so the restored
# container kept reading the pristine image copy of /etc/ld.so.cache -> the
# loader resolved the wrong libcuda. The fix populates the upperdir before the
# overlay is mounted (in the containerd shim / daemon).

# bats test_tags=dump,restore,rwlayer
@test "Restore: RW-layer file shadows image copy (overlay coherence)" {
    local marker="CEDANA_RW_LAYER_OK"

    # Overwrite an existing image file so the new content lands in the RW (upper)
    # layer, shadowing the image's copy in the lower layer.
    local script
    script=$(
        cat <<EOF
set -e
echo ${marker} > /etc/debian_version
echo RW_TEST_READY
while true; do date; sleep 1; done
EOF
    )

    local spec
    spec=$(cmd_pod_spec "debian:stable-slim" "$script")

    local name original_name pod_id action_id restore_action_id restored

    kubectl apply -f "$spec"
    name=$(get_created_pod "$spec" "$NAMESPACE" 30)
    [ -n "$name" ]
    validate_pod "$name" 120
    original_name="$name"
    wait_for_log_trigger "$name" "RW_TEST_READY" 120 "$NAMESPACE"

    pod_id=$(get_pod_id "$name" "$NAMESPACE")
    action_id=$(checkpoint_pod "$pod_id")
    validate_action_id "$action_id"
    poll_action_status "$action_id" "checkpoint" 120

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
    restore_action_id=$(restore_pod "$action_id" "$CLUSTER_ID")
    validate_action_id "$restore_action_id"
    restored=$(wait_for_cmd 60 get_restored_pod "$original_name" "$NAMESPACE")
    [ -n "$restored" ]
    validate_pod "$restored" 120

    # A fresh exec reads the file through the merged overlay
    run kubectl exec "$restored" -n "$NAMESPACE" -- cat /etc/debian_version
    if [ "$status" -ne 0 ] || ! echo "$output" | grep -q "$marker"; then
        error_log "restored /etc/debian_version is stale ('$marker' missing) -> RW upper layer not visible through the overlay (coherence regression)"
        echo "$output"
        kubectl delete pod "$restored" -n "$NAMESPACE" || true
        return 1
    fi

    kubectl delete pod "$restored" -n "$NAMESPACE" || true
}
