#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,declarative

load ../helpers/utils
load ../helpers/daemon
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

CHECKPOINTED_AT=10

labelled_pod_spec() {
    local name="$1"
    local label="$2"

    local spec
    spec=/tmp/pod-${name}-$(unix_nano).yaml
    cat >"$spec" <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: "$NAMESPACE"
  labels:
    app: "$name"
    cedana-e2e-test: "$TEST_ID"
spec:
  runtimeClassName: cedana
  containers:
  - name: counter
    image: alpine:latest
    command: ["/bin/sh", "-c"]
    args:
      - |
        i=0
        while true; do echo "COUNT \$i"; i=\$((i+1)); sleep 1; done
    env:
    - name: CEDANA_CHECKPOINT
      value: "$label"
EOF
    echo "$spec"
}

first_count() {
    kubectl logs "$1" -n "$NAMESPACE" 2>/dev/null | grep -m1 -oE 'COUNT [0-9]+' | awk '{print $2}'
}

deploy_counting_pod() {
    local spec="$1"
    local name="$2"

    kubectl apply -f "$spec"
    validate_pod "$name" 120
    wait_for_log_trigger "$name" "COUNT $CHECKPOINTED_AT" 120
}

wait_for_checkpoint() {
    local action_id="$1"

    validate_action_id "$action_id"
    poll_action_status "$action_id" "checkpoint" 180
}

assert_restored() {
    local name="$1"

    validate_pod "$name" 180
    wait_for_log_trigger "$name" "COUNT" 60

    local first
    first=$(first_count "$name")
    if [ -z "$first" ] || [ "$first" -lt "$CHECKPOINTED_AT" ]; then
        error_log "pod $name cold-started (first count '${first:-none}', expected >= $CHECKPOINTED_AT): CEDANA_CHECKPOINT was not restored"
        kubectl logs "$name" -n "$NAMESPACE" --tail=20 2>/dev/null || true
        return 1
    fi
    debug_log "pod $name restored at count $first"
}

setup() {
    TEST_ID=$(unix_nano)
}

teardown() {
    kubectl delete pod -n "$NAMESPACE" -l cedana-e2e-test="$TEST_ID" --wait=false 2>/dev/null || true
}

# bats test_tags=restore,declarative
@test "Restore: CEDANA_CHECKPOINT restores a pod recreated under the same name" {
    local id label name spec action_id
    id=$TEST_ID
    label="e2e-same-name-$id"
    name="decl-same-$id"
    spec=$(labelled_pod_spec "$name" "$label")

    deploy_counting_pod "$spec" "$name"
    action_id=$(checkpoint_pod_by_name "$name" "$NAMESPACE")
    wait_for_checkpoint "$action_id"

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
    kubectl apply -f "$spec"

    assert_restored "$name"
}

@test "Restore: CEDANA_CHECKPOINT restores a checkpoint requested by pod name" {
    local id label source replica action_id
    id=$TEST_ID
    label="e2e-by-name-$id"
    source="decl-src-$id"
    replica="decl-replica-$id"

    deploy_counting_pod "$(labelled_pod_spec "$source" "$label")" "$source"
    action_id=$(checkpoint_pod_by_name "$source" "$NAMESPACE")
    wait_for_checkpoint "$action_id"

    kubectl delete pod "$source" -n "$NAMESPACE" --wait=true
    kubectl apply -f "$(labelled_pod_spec "$replica" "$label")"

    assert_restored "$replica"
}

# bats test_tags=restore,declarative,scaleup
@test "Restore: CEDANA_CHECKPOINT restores a new replica while the source pod is still running" {

    local id label source replica action_id
    id=$TEST_ID
    label="e2e-scale-up-$id"
    source="decl-scale-src-$id"
    replica="decl-scale-replica-$id"

    deploy_counting_pod "$(labelled_pod_spec "$source" "$label")" "$source"
    action_id=$(checkpoint_pod "$(get_pod_id "$source" "$NAMESPACE")")
    wait_for_checkpoint "$action_id"

    kubectl apply -f "$(labelled_pod_spec "$replica" "$label")"

    assert_restored "$replica"
    validate_pod "$source" 30
}
