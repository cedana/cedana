#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=k8s,kubernetes,aws,AWS,eks,EKS

# NOTE: Set defaults to use remote checkpoint storage with good compression
# as this test suite will always run on a remote cluster.
export CEDANA_CHECKPOINT_DIR=${CEDANA_CHECKPOINT_DIR:-cedana://ci}
export CEDANA_CHECKPOINT_COMPRESSION=${CEDANA_CHECKPOINT_COMPRESSION:-lz4}

load ../helpers/utils
load ../helpers/daemon # required for config env vars
load ../helpers/aws
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

CLUSTER_NAME="test-eks-$(unix_nano)"
export CLUSTER_NAME
export CLUSTER_ID
export NAMESPACE="test"
export CEDANA_NAMESPACE="cedana-system"

setup_file() {
    setup_cluster
    tail_all_logs $CEDANA_NAMESPACE 300 &
    TAIL_PID=$!
    CLUSTER_ID=$(register_cluster "$CLUSTER_NAME")
    helm_install_cedana "$CLUSTER_ID" "$CEDANA_NAMESPACE"
    wait_for_ready "$CEDANA_NAMESPACE" 300
    create_namespace "$NAMESPACE"
}

teardown_file() {
    if [ -n "$TAIL_PID" ]; then
        kill "$TAIL_PID"
    fi
    delete_namespace "$NAMESPACE" --force
    # Clean up any leftover PVs from tests
    kubectl delete pv --all --wait=false || true
    helm_uninstall_cedana $CEDANA_NAMESPACE
    teardown_cluster &> /dev/null
    deregister_cluster "$CLUSTER_ID"
}

teardown() {
    if [ "$DEBUG" != '1' ]; then
        error all_logs "$CEDANA_NAMESPACE" 120 1000
    fi
}

@test "Verify cluster and helm installation" {
    # Test that cluster is running
    run kubectl get nodes
    [ "$status" -eq 0 ]
    [[ "$output" == *"Ready"* ]]

    # Test that Cedana components are running
    kubectl get pods -n $CEDANA_NAMESPACE

    # Check if all Cedana pods are actually ready
    kubectl wait --for=condition=Ready pod -l app.kubernetes.io/instance=cedana -n $CEDANA_NAMESPACE --timeout=300s

    validate_propagator_connectivity
}

# bats test_tags=deploy
@test "Deploy a pod" {
    local name
    name=$(unix_nano)
    local script
    script=$(cat "$WORKLOADS"/date-loop.sh)
    local spec
    spec=$(cmd_pod_spec "$NAMESPACE" "$name" "alpine:latest" "$script")

    kubectl apply -f "$spec"

    sleep 5

    # Check if pod is running
    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=300s -n "$NAMESPACE"

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
}

# bats test_tags=dump
@test "Checkpoint a pod (wait for completion, streams=$CEDANA_CHECKPOINT_STREAMS)" {
    local name
    name=$(unix_nano)
    local script
    script=$(cat "$WORKLOADS"/date-loop.sh)
    local spec
    spec=$(cmd_pod_spec "$NAMESPACE" "$name" "alpine:latest" "$script")

    kubectl apply -f "$spec"

    sleep 5

    # Check if pod is running
    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=300s -n "$NAMESPACE"

    pod_id=$(get_pod_id "$name" "$NAMESPACE")
    run checkpoint_pod "$pod_id"
    [ "$status" -eq 0 ]

    if [ $status -eq 0 ]; then
        action_id=$output
        validate_action_id "$action_id"

        poll_action_status "$action_id" "checkpoint"
    fi

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
}

# bats test_tags=restore
@test "Restore a pod with original pod running (wait until running, streams=$CEDANA_CHECKPOINT_STREAMS)" {
    local name
    name=$(unix_nano)
    local script
    script=$(cat "$WORKLOADS"/date-loop.sh)
    local spec
    spec=$(cmd_pod_spec "$NAMESPACE" "$name" "alpine:latest" "$script")

    kubectl apply -f "$spec"

    sleep 5

    # Check if pod is running
    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=300s -n "$NAMESPACE"

    pod_id=$(get_pod_id "$name" "$NAMESPACE")
    run checkpoint_pod "$pod_id"
    [ "$status" -eq 0 ]

    if [ $status -eq 0 ]; then
        action_id=$output
        validate_action_id "$action_id"

        poll_action_status "$action_id" "checkpoint"

        run restore_pod "$action_id" "$CLUSTER_ID"
        [ "$status" -eq 0 ]

        if [ $status -eq 0 ]; then
            action_id="$output"
            validate_action_id "$action_id"

            run wait_for_cmd 30 get_restored_pod "$NAMESPACE" "$name"
            [ "$status" -eq 0 ]

            if [ $status -eq 0 ]; then
                local restored_pod="$output"
                validate_pod "$NAMESPACE" "$restored_pod" 20s

                kubectl delete pod "$restored_pod" -n "$NAMESPACE" --wait=true
            fi
        fi
    fi

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
}

# bats test_tags=restore
@test "Restore a pod with original pod deleted (wait until running, streams=$CEDANA_CHECKPOINT_STREAMS)" {
    local name
    name=$(unix_nano)
    local script
    script=$(cat "$WORKLOADS"/date-loop.sh)
    local spec
    spec=$(cmd_pod_spec "$NAMESPACE" "$name" "alpine:latest" "$script")

    kubectl apply -f "$spec"

    sleep 5

    # Check if pod is running
    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=300s -n "$NAMESPACE"

    pod_id=$(get_pod_id "$name" "$NAMESPACE")
    run checkpoint_pod "$pod_id"
    [ "$status" -eq 0 ]

    if [ $status -eq 0 ]; then
        action_id=$output
        validate_action_id "$action_id"

        poll_action_status "$action_id" "checkpoint"

        kubectl delete pod "$name" -n "$NAMESPACE" --wait=true

        run restore_pod "$action_id" "$CLUSTER_ID"
        [ "$status" -eq 0 ]

        if [ $status -eq 0 ]; then
            action_id="$output"
            validate_action_id "$action_id"

            run wait_for_cmd 30 get_restored_pod "$NAMESPACE" "$name"
            [ "$status" -eq 0 ]

            if [ $status -eq 0 ]; then
                local restored_pod="$output"
                validate_pod "$NAMESPACE" "$restored_pod" 20s

                kubectl delete pod "$restored_pod" -n "$NAMESPACE" --wait=true
            fi
        fi
    fi
}

# bats test_tags=restore,pvc
@test "Checkpoint and restore pod with PVC (wait until running, streams=$CEDANA_CHECKPOINT_STREAMS)" {
    skip "Skipped until it supports running parallely with other tests"

    local pv_name="counting-pv"
    local pvc_name="counting-pvc"
    local pod_name="counting-pvc-pod"

    kubectl apply -f /cedana-samples/kubernetes/counting-persistent-volume.yaml -n "$NAMESPACE"

    kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$pod_name" --timeout=300s -n "$NAMESPACE"

    pod_id=$(get_pod_id "$pod_name" "$NAMESPACE")
    run checkpoint_pod "$pod_id"
    [ "$status" -eq 0 ]

    if [ $status -eq 0 ]; then
        action_id=$output
        validate_action_id "$action_id"

        poll_action_status "$action_id" "checkpoint"

        kubectl delete pod "$pod_name" -n "$NAMESPACE" --wait=true

        run restore_pod "$action_id" "$CLUSTER_ID"
        [ "$status" -eq 0 ]

        if [ $status -eq 0 ]; then
            action_id="$output"
            validate_action_id "$action_id"

            run wait_for_cmd 30 get_restored_pod "$NAMESPACE" "$name"
            [ "$status" -eq 0 ]

            if [ $status -eq 0 ]; then
                local restored_pod="$output"
                validate_pod "$NAMESPACE" "$restored_pod" 20s

                kubectl get pvc "$pvc_name" -n "$NAMESPACE" -o jsonpath='{.status.phase}'

                run kubectl get pod "$restored_pod" -n "$NAMESPACE" -o jsonpath='{.spec.volumes[*].persistentVolumeClaim.claimName}'
                [ "$status" -eq 0 ]
                [[ "$output" == *"$pvc_name"* ]]

                kubectl delete pod "$restored_pod" -n "$NAMESPACE" --wait=true
            fi
        fi
    fi

    run kubectl delete pvc "$pvc_name" -n "$NAMESPACE" --wait=true
    run kubectl delete pv "$pv_name" --wait=true
}
