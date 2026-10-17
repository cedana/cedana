#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,lifecycle

# Edge cases of a pod's lifecycle around checkpoint/restore, as customers hit them.
# Each one must end in a real restore or an explicit, timely failure: never a hang,
# and never a silent cold start.

load ../helpers/utils
load ../helpers/daemon
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

CHECKPOINTED_AT=10

# A counter pod. With a grace period, it ignores SIGTERM, so it stays Terminating that long
# after it's deleted.
counter_pod_spec() {
    local grace="${1:-}"

    local name
    name="test-lifecycle-$(unix_nano)"
    local spec=/tmp/pod-${name}.yaml
    local on_term="exit 0"
    if [ -n "$grace" ]; then
        on_term=""
    fi
    cat >"$spec" <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: "$NAMESPACE"
  labels:
    app: "$name"
spec:
  terminationGracePeriodSeconds: ${grace:-30}
  containers:
  - name: "$name"
    image: alpine:latest
    command: ["/bin/sh", "-c"]
    args:
      - |
        trap '$on_term' TERM
        i=0
        while true; do echo "COUNT \$i"; i=\$((i+1)); sleep 1; done
EOF
    echo "$spec"
}

# NOTE: these helpers run in $(...), where set -e doesn't apply, so each step fails explicitly

# Deploys the pod, and waits for it to count past CHECKPOINTED_AT. Prints the pod name.
deploy_counter() {
    local spec="$1"
    local name
    kubectl apply -f "$spec" >&2 || return 1
    name=$(get_created_pod "$spec" "$NAMESPACE" 30) || return 1
    validate_pod "$name" 120 >&2 || return 1
    wait_for_log_trigger "$name" "COUNT $CHECKPOINTED_AT" 120 >&2 || return 1
    echo "$name"
}

# Checkpoints the pod, and waits for the checkpoint to finish. Prints the action ID.
checkpoint_counter() {
    local name="$1"
    local directory="${2:-}"

    local pod_id action_id
    pod_id=$(get_pod_id "$name" "$NAMESPACE") || return 1
    action_id=$(checkpoint_pod "$pod_id" "" "$directory") || return 1
    validate_action_id "$action_id" >&2 || return 1
    poll_action_status "$action_id" "checkpoint" 180 >&2 || return 1
    echo "$action_id"
}

# Starts a restore of the checkpoint, and waits for its pod. Prints the restore ID and pod.
start_restore() {
    local action_id="$1"
    local original_name="$2"

    local restore_id restored
    restore_id=$(restore_pod "$action_id" "$CLUSTER_ID") || return 1
    validate_action_id "$restore_id" >&2 || return 1
    restored=$(wait_for_cmd 120 get_restored_pod "$original_name" "$NAMESPACE") || true
    echo "$restore_id $restored"
}

# Pods this test created (only those: other files' tests run in the same namespace)
TEST_PODS=()

teardown() {
    for pod in "${TEST_PODS[@]}"; do
        # Also a restored pod that appeared after the test stopped waiting for it
        kubectl delete pod "$pod" "${pod%-restored}-restored" -n "$NAMESPACE" --force --grace-period=0 --wait=false &>/dev/null || true
    done
    if [ -n "${CHECKPOINT_DIR_OVERRIDE:-}" ]; then
        rm -rf "$CHECKPOINT_DIR_OVERRIDE"
    fi
}

# A restore that overlaps the source pod's termination must not wait for it, e.g. for
# resources it still holds. Covers a restore hanging until its 10 minute timeout while the
# source pod terminated on the same node (C20).
# bats test_tags=restore
@test "Restore while the source pod is still terminating" {
    local grace=180
    local budget=120 # well within the grace period, so the source is Terminating throughout

    local spec name action_id
    spec=$(counter_pod_spec "$grace")
    name=$(deploy_counter "$spec")
    TEST_PODS+=("$name")
    action_id=$(checkpoint_counter "$name")

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=false
    wait_for_cmd 30 "[ -n \"\$(kubectl get pod $name -n $NAMESPACE -o jsonpath='{.metadata.deletionTimestamp}')\" ]"

    local start=$SECONDS
    local restore_id restored out
    out=$(start_restore "$action_id" "$name")
    read -r restore_id restored <<<"$out"
    if [ -n "$restored" ]; then TEST_PODS+=("$restored"); fi
    [ -n "$restored" ] || {
        error_log "Restored pod for $name did not appear"
        return 1
    }

    validate_pod "$restored" "$budget"
    poll_restore_status "$restore_id" "$budget"
    local took=$((SECONDS - start))
    verify_restored_pod "$restored" "$NAMESPACE"

    if [ "$took" -gt "$budget" ]; then
        error_log "Restore took ${took}s while the source pod was terminating (budget ${budget}s)"
        return 1
    fi
    # Otherwise the restore didn't overlap the termination, which is the point
    if ! kubectl get pod "$name" -n "$NAMESPACE" &>/dev/null; then
        error_log "Source pod $name was gone before the restore finished (${took}s), so they didn't overlap"
        return 1
    fi

    local first
    first=$(first_count "$restored")
    if [ -z "$first" ] || [ "$first" -lt "$CHECKPOINTED_AT" ]; then
        error_log "Pod $restored cold-started (first count '${first:-none}', checkpointed at >= $CHECKPOINTED_AT)"
        return 1
    fi
    debug_log "Restored $restored in ${took}s at count $first, while $name was terminating"
}

# A restore that can't work must say so quickly, instead of retrying or hanging until a
# timeout. Covers C4.
# bats test_tags=restore
@test "Restore of a corrupted checkpoint fails fast" {
    # The checkpoint must be on this host, to corrupt it
    if [ "$PROVIDER" != "k3s" ] || [[ "$CEDANA_CHECKPOINT_DIR" != /* ]]; then
        skip "needs local checkpoint storage on a K3s node"
    fi
    local budget=90

    CHECKPOINT_DIR_OVERRIDE="$CEDANA_CHECKPOINT_DIR/cedana-lifecycle-$(unix_nano)"
    mkdir -p "$CHECKPOINT_DIR_OVERRIDE"

    local spec name action_id
    spec=$(counter_pod_spec)
    name=$(deploy_counter "$spec")
    TEST_PODS+=("$name")
    action_id=$(checkpoint_counter "$name" "$CHECKPOINT_DIR_OVERRIDE")

    # Cut every file of the checkpoint in half
    local files
    files=$(find "$CHECKPOINT_DIR_OVERRIDE" -type f)
    if [ -z "$files" ]; then
        error_log "The checkpoint isn't in the directory it was asked for ($CHECKPOINT_DIR_OVERRIDE)"
        ls -la "$CEDANA_CHECKPOINT_DIR" >&2
        return 1
    fi
    while IFS= read -r file; do
        truncate -s $(($(stat -c %s "$file") / 2)) "$file"
    done <<<"$files"
    debug_log "Corrupted $(wc -l <<<"$files") checkpoint files"

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true

    local start=$SECONDS
    local restore_id restored out
    out=$(start_restore "$action_id" "$name")
    read -r restore_id restored <<<"$out"
    if [ -n "$restored" ]; then TEST_PODS+=("$restored"); fi

    local status=""
    while [ $((SECONDS - start)) -lt "$budget" ]; do
        status=$(get_restore_status "$restore_id")
        case "$status" in
            success)
                error_log "Restore $restore_id of a corrupted checkpoint reported success"
                return 1
                ;;
            fallback | error | failed)
                break
                ;;
        esac
        sleep 5
    done
    local took=$((SECONDS - start))

    case "$status" in
        fallback | error | failed) ;;
        *)
            error_log "Restore $restore_id of a corrupted checkpoint didn't fail within ${budget}s (status: ${status:-not recorded})"
            [ -n "$restored" ] && kubectl describe pod "$restored" -n "$NAMESPACE" >&2
            return 1
            ;;
    esac
    debug_log "Restore of the corrupted checkpoint ended as '$status' in ${took}s"

    # A fallback cold-starts: the pod must then run from scratch, not from corrupted state
    if [ "$status" = "fallback" ] && [ -z "$restored" ]; then
        error_log "Restore $restore_id fell back, but its pod never appeared"
        return 1
    fi
    if [ "$status" = "fallback" ]; then
        validate_pod "$restored" 60
        wait_for_log_trigger "$restored" "COUNT" 30
        local first
        first=$(first_count "$restored")
        if [ "${first:-}" != "0" ]; then
            error_log "Pod $restored fell back, but didn't start from scratch (first count '${first:-none}')"
            return 1
        fi
    fi
}
