#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes

load ../helpers/utils
load ../helpers/daemon
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

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
#
# This reproduces the exact mechanism WITHOUT GPUs: the container regenerates
# /etc/ld.so.cache (so the new cache lives in the RW/upper layer, shadowing the
# image's lower copy), we checkpoint + restore, then a FRESH `ldconfig -p` in the
# restored container reads the cache through the merged overlay:
#   - PASS (fixed):   upper cache is served  -> the added entry is present.
#   - FAIL (pre-fix): stale lower cache served -> the added entry is missing.

# bats test_tags=dump,restore,rwlayer
@test "Restore: RW-layer ld.so.cache shadows image copy (overlay coherence)" {
    local marker="libcedanarwtest"

    # Regenerate the loader cache inside the container: adding a lib dir + running
    # ldconfig rewrites /etc/ld.so.cache into the upper layer, shadowing the
    # image's copy in the lower layer
    local script
    script=$(
        cat <<EOF
set -e
mkdir -p /opt/${marker}
cp /lib/x86_64-linux-gnu/libc.so.6 /opt/${marker}/${marker}.so.1
echo /opt/${marker} > /etc/ld.so.conf.d/${marker}.conf
ldconfig
ldconfig -p | grep -q ${marker} || { echo "setup failed: marker missing from cache before checkpoint"; exit 1; }
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

    # A fresh exec reads /etc/ld.so.cache through the merged overlay
    run kubectl exec "$restored" -n "$NAMESPACE" -- ldconfig -p
    if [ "$status" -ne 0 ] || ! echo "$output" | grep -q "$marker"; then
        error_log "restored loader cache is missing '$marker' -> RW upper layer not visible through the overlay (coherence regression)"
        echo "$output"
        kubectl delete pod "$restored" -n "$NAMESPACE" || true
        return 1
    fi

    kubectl delete pod "$restored" -n "$NAMESPACE" || true
}
