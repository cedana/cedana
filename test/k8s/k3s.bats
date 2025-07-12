#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=k8s,k3s,remote

load ../helpers/utils
load ../helpers/daemon # required for config env vars
load ../helpers/k3s
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

CLUSTER_NAME="test-$(unix_nano)"
export CLUSTER_NAME
export CLUSTER_ID
export NAMESPACE="default"
export CEDANA_NAMESPACE="cedana-system"
export RUNC_ROOT="/run/containerd/runc/k8s.io"

setup_file() {
    setup_k3s_cluster
    helm_install_cedana "$CLUSTER_NAME" $CEDANA_NAMESPACE
    CLUSTER_ID=$(cluster_id "$CLUSTER_NAME")
}

teardown_file() {
    helm_uninstall_cedana $CEDANA_NAMESPACE
    teardown_k3s_cluster
}

@test "Verify cluster and helm installation" {
    # Test that k3s cluster is running
    run kubectl get nodes
    [ "$status" -eq 0 ]
    [[ "$output" == *"Ready"* ]]

    # Test that Cedana components are running
    run kubectl get pods -n $CEDANA_NAMESPACE
    [ "$status" -eq 0 ]

    # Check if all Cedana pods are actually ready
    run kubectl wait --for=condition=Ready pod -l app.kubernetes.io/instance=cedana -n $CEDANA_NAMESPACE --timeout=120s
    [ "$status" -eq 0 ]

    run validate_propagator_connectivity
    [ "$status" -eq 0 ]
}

# bats test_tags=deploy
@test "Deploy a pod" {
    local name
    name=$(unix_nano)
    local spec
    spec=$(new_spec /cedana-samples/kubernetes/counting.yaml "$name")

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=120s -n "$NAMESPACE"
    [ "$status" -eq 0 ]

    sleep 100

    run kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
    [ "$status" -eq 0 ]
}

# bats test_tags=dump
@test "Checkpoint a pod (wait for completion)" {
    local name
    name=$(unix_nano)
    local spec
    spec=$(new_spec /cedana-samples/kubernetes/counting.yaml "$name")
    local action_id

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=120s -n "$NAMESPACE"
    [ "$status" -eq 0 ]

    # Checkpoint the test pod
    run checkpoint_pod "$name" "$RUNC_ROOT" "$NAMESPACE"
    [ "$status" -eq 0 ]

    action_id=$output

    if [ $status -eq 0 ]; then
        run validate_action_id "$action_id"
        [ $status -eq 0 ]

        run poll_action_status "$action_id" "checkpoint"
        [ "$status" -eq 0 ]
    fi

    run kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
    [ "$status" -eq 0 ]
}

# bats test_tags=restore
@test "Restore a pod with original pod running (wait until running)" {
    local name
    name=$(unix_nano)
    local spec
    spec=$(new_spec /cedana-samples/kubernetes/counting.yaml "$name")
    local action_id

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=120s -n "$NAMESPACE"
    [ "$status" -eq 0 ]

    # Checkpoint the test pod
    run checkpoint_pod "$name" "$RUNC_ROOT" "$NAMESPACE"
    [ "$status" -eq 0 ]

    action_id=$output

    if [ $status -eq 0 ]; then
        run validate_action_id "$action_id"
        [ $status -eq 0 ]

        run poll_action_status "$action_id" "checkpoint"
        [ "$status" -eq 0 ]
    fi

    run restore_pod "$action_id" "$CLUSTER_ID"
    [ $status -eq 0 ]

    if [ $status -eq 0 ]; then
        action_id="$output"
        run validate_action_id "$action_id"
        [ $status -eq 0 ]

        run get_restored_pod "$NAMESPACE" "$name"
        [ $status -eq 0 ]

        if [ $status -eq 0 ]; then
            local restored_pod="$output"
            run validate_pod "$NAMESPACE" "$restored_pod" 20s
            [ $status -eq 0 ]

            run kubectl delete pod "$restored_pod" -n "$NAMESPACE" --wait=true
            [ "$status" -eq 0 ]
        fi
    fi

    run kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
    [ "$status" -eq 0 ]
}

# bats test_tags=restore
@test "Restore a pod with original pod deleted (wait until running)" {
    local name
    name=$(unix_nano)
    local spec
    spec=$(new_spec /cedana-samples/kubernetes/counting.yaml "$name")
    local action_id

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=120s -n "$NAMESPACE"
    [ "$status" -eq 0 ]

    # Checkpoint the test pod
    run checkpoint_pod "$name" "$RUNC_ROOT" "$NAMESPACE"
    [ "$status" -eq 0 ]

    action_id=$output

    if [ $status -eq 0 ]; then
        run validate_action_id "$action_id"
        [ $status -eq 0 ]

        run poll_action_status "$action_id" "checkpoint"
        [ "$status" -eq 0 ]
    fi

    run kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
    [ "$status" -eq 0 ]

    run restore_pod "$action_id" "$CLUSTER_ID"
    [ $status -eq 0 ]

    if [ $status -eq 0 ]; then
        action_id="$output"
        run validate_action_id "$action_id"
        [ $status -eq 0 ]

        run get_restored_pod "$NAMESPACE" "$name"
        [ $status -eq 0 ]

        if [ $status -eq 0 ]; then
            local restored_pod="$output"
            run validate_pod "$NAMESPACE" "$restored_pod" 20s
            [ $status -eq 0 ]

            run kubectl delete pod "$restored_pod" -n "$NAMESPACE" --wait=true
            [ "$status" -eq 0 ]
        fi
    fi
}
