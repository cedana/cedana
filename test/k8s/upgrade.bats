#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,upgrade

# Upgrading the way customers do: from the previous release to this one, with a checkpoint
# taken before the upgrade. With UPGRADE_FROM_CHART set (e.g. "latest"), the suite starts from
# that released chart and upgrades to this one before any test (see helm_upgrade_cedana), so
# these only check the upgrade, and every other test runs after it.

load ../helpers/utils
load ../helpers/daemon
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

HELPER_DAEMONSET=cedana-helper

setup_file() {
    if [ -z "${UPGRADE_FROM_CHART:-}" ]; then
        skip "UPGRADE_FROM_CHART is not set"
    fi
}

# Pods this test created (only those: other files' tests run in the same namespace)
TEST_PODS=()

teardown() {
    for pod in "${TEST_PODS[@]}"; do
        # Also a restored pod that appeared after the test stopped waiting for it
        kubectl delete pod "$pod" "${pod%-restored}-restored" -n "$NAMESPACE" --force --grace-period=0 --wait=false &>/dev/null || true
    done
}

# Every helper pod runs the given image digest, and is Ready
helpers_on_digest() {
    local digest="$1"
    local pods
    pods=$(kubectl get pods -n "$CEDANA_NAMESPACE" -o json |
        jq -r --arg ds "$HELPER_DAEMONSET" '.items[] | select(.metadata.ownerReferences[]?.name == $ds)')
    [ -n "$pods" ] || return 1
    # Not on the digest, or not Ready
    ! jq -e --arg d "$digest" 'select(
        ([.status.containerStatuses[]? | select(.name == "helper") | .imageID | contains($d)] | all | not) or
        ([.status.conditions[]? | select(.type == "Ready") | .status == "True"] | any | not)
    )' <<<"$pods" >/dev/null
}

@test "Upgrade from the previous release runs this build on every node" {
    # The upgrade landed: helm can leave a DaemonSet patched in place on the old image
    if [ -n "${HELPER_DIGEST:-}" ]; then
        wait_for_cmd 120 helpers_on_digest "$HELPER_DIGEST" || {
            error_log "Not every helper is Ready on the upgraded image ($HELPER_DIGEST):"
            kubectl get pods -n "$CEDANA_NAMESPACE" -o wide >&2
            return 1
        }
    fi

    # It runs the cedana under test, not some other build (e.g. a stale one downloaded)
    local expected actual
    # The binary under test (built or downloaded, like the helper image), else git describe
    expected=$( { [ -x ./cedana ] && ./cedana version; } || git -C "$BATS_TEST_DIRNAME/../.." describe --tags --always)
    actual=$(kubectl exec -n "$CEDANA_NAMESPACE" daemonset/"$HELPER_DAEMONSET" -c helper -- cedana version | tr -d '[:space:]')
    if [ "$actual" != "$expected" ]; then
        error_log "The upgraded helper runs cedana $actual, but this is $expected"
        return 1
    fi
}

# bats test_tags=restore
@test "Restore a checkpoint taken before the upgrade from the previous release" {
    local name="$UPGRADE_POD" count="$UPGRADE_COUNT" from="$UPGRADE_FROM"

    # The old checkpoint restores, or is refused explicitly: never a silent cold start or a hang
    local restore_id restored
    restore_id=$(restore_pod "$UPGRADE_ACTION_ID" "$CLUSTER_ID")
    validate_action_id "$restore_id"
    restored=$(wait_for_cmd 60 get_restored_pod "$name" "$NAMESPACE") || true
    if [ -n "$restored" ]; then TEST_PODS+=("$restored"); fi

    local status="" start=$SECONDS
    while [ $((SECONDS - start)) -lt 300 ]; do
        status=$(get_restore_status "$restore_id")
        case "$status" in success | error | failed | fallback) break ;; esac
        sleep 5
    done

    case "$status" in
        success)
            validate_pod "$restored" 120
            verify_restored_pod "$restored" "$NAMESPACE"
            wait_for_cmd 30 "kubectl logs $restored -n $NAMESPACE | grep -qE '^COUNT [0-9]+'"
            local first
            first=$(first_count "$restored")
            if [ -z "$first" ] || [ "$first" -lt "$count" ]; then
                error_log "Restored pod $restored started at count '${first:-none}', but it was at $count before the checkpoint"
                return 1
            fi
            debug_log "Restored the $from checkpoint after the upgrade, at count $first"
            ;;
        error | failed)
            debug_log "The $from checkpoint was refused after the upgrade ($status), explicitly"
            ;;
        fallback)
            error_log "The $from checkpoint silently cold-started after the upgrade (status fallback)"
            return 1
            ;;
        *)
            error_log "Restore of the $from checkpoint after the upgrade didn't finish in 300s (status: ${status:-not recorded})"
            return 1
            ;;
    esac
}
