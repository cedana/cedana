#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=k8s,eks

load ../helpers/utils
load ../helpers/daemon
load ../helpers/eks
load ../helpers/helm
load ../helpers/propagator
load ../helpers/k8s

export CLUSTER_NAME="cedana-ci-$(unix_nano)"
export NAMESPACE="default"
export CLUSTER_ID
export CEDANA_NAMESPACE="cedana-system"
export RUNC_ROOT="/run/containerd/runc/k8s.io"

setup_file() {
    setup_eks_cluster
    wait_for_eks_node_groups
    install_nvidia_gpu_operator
    verify_gpu_operator
    helm_install_cedana_eks $CLUSTER_NAME $CEDANA_NAMESPACE
    tail_helm_cedana_logs &
    CLUSTER_ID=$(cluster_id "$CLUSTER_NAME")
}

teardown_file() {
    helm_uninstall_cedana $CEDANA_NAMESPACE
    teardown_eks_cluster
}

@test "Verify EKS cluster and Cedana installation" {
    # Test that EKS cluster is running
    run kubectl get nodes
    [ "$status" -eq 0 ]
    [[ "$output" == *"Ready"* ]]

    # Test that Cedana components are running
    run kubectl get pods -n $CEDANA_NAMESPACE
    [ "$status" -eq 0 ]

    # Check if all Cedana pods are actually ready
    run kubectl wait --for=condition=Ready pod -l app.kubernetes.io/instance=cedana -n $CEDANA_NAMESPACE --timeout=600s
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
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=600s -n "$NAMESPACE"
    [ "$status" -eq 0 ]

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
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=600s -n "$NAMESPACE"
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
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=600s -n "$NAMESPACE"
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
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=600s -n "$NAMESPACE"
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


