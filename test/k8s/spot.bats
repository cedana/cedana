#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,spot,karpenter

load ../helpers/utils
load ../helpers/daemon
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

setup_file() {
    if [ "${PROVIDER:-}" != "eks-karpenter" ]; then
        skip "Spot tests require PROVIDER=eks-karpenter"
    fi
}

# bats test_tags=spot
@test "Checkpoint and restore across spot interruption" {
    local script
    local spec

    script=$(cat "$WORKLOADS"/date-loop.sh)
    spec=$(spot_pod_spec "alpine:latest" "$script")

    # Deploy and wait for spot node
    kubectl apply -f "$spec"
    local name
    name=$(get_created_pod "$spec" "$NAMESPACE" 60)

    local node_name
    node_name=$(wait_for_spot_node "$name" "$NAMESPACE" 300)
    validate_pod "$name" 300

    # Let workload run, then checkpoint
    sleep 30
    local pod_id
    pod_id=$(get_pod_id "$name" "$NAMESPACE")
    local action_id
    action_id=$(checkpoint_pod "$pod_id")
    validate_action_id "$action_id"
    poll_action_status "$action_id" "checkpoint" 120

    # Simulate spot interruption
    simulate_spot_interruption "$node_name"
    wait_for_cmd_fail 180 "kubectl get pod $name -n $NAMESPACE -o name 2>/dev/null | grep -q ."

    # Restore
    local restore_action_id
    restore_action_id=$(restore_pod "$action_id" "$CLUSTER_ID")
    validate_action_id "$restore_action_id"

    local restored_name
    restored_name=$(wait_for_cmd 120 get_restored_pod "$name" "$NAMESPACE")
    validate_pod "$restored_name" 300

    # Cleanup
    kubectl delete pod "$restored_name" -n "$NAMESPACE" --ignore-not-found=true
}
