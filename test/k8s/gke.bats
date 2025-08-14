#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=k8s,kubernetes,gcp,GCP,gke,GKE

# Set defaults to use remote checkpoint storage with good compression
# as this test suite will always run on a remote cluster.
export CEDANA_CHECKPOINT_DIR=${CEDANA_CHECKPOINT_DIR:-cedana://}
export CEDANA_CHECKPOINT_COMPRESSION=${CEDANA_CHECKPOINT_COMPRESSION:-lz4}

load ../helpers/utils
load ../helpers/daemon # required for config env vars
load ../helpers/gcp
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

CLUSTER_NAME="test-$(unix_nano)"
export CLUSTER_NAME
export CLUSTER_ID
export NAMESPACE="test"
export CEDANA_NAMESPACE="cedana-system"
export RUNC_ROOT="/run/containerd/runc/k8s.io"

setup_file() {
    setup_cluster
    tail_all_logs $CEDANA_NAMESPACE 120 &
    helm_install_cedana "$CLUSTER_NAME" $CEDANA_NAMESPACE
    wait_for_ready "$CEDANA_NAMESPACE" 120
    CLUSTER_ID=$(wait_for_cmd 120 cluster_id "$CLUSTER_NAME")
    create_namespace "$NAMESPACE"
}

teardown_file() {
    delete_namespace "$NAMESPACE" --force
    helm_uninstall_cedana $CEDANA_NAMESPACE
    teardown_cluster
}

teardown() {
    error all_logs "$CEDANA_NAMESPACE" 120 1000
}

@test "Verify cluster and helm installation" {
    # Test that cluster is running
    run kubectl get nodes
    [ "$status" -eq 0 ]
    [[ "$output" == *"Ready"* ]]

    # Test that Cedana components are running
    run kubectl get pods -n $CEDANA_NAMESPACE
    [ "$status" -eq 0 ]

    # Check if all Cedana pods are actually ready
    run kubectl wait --for=condition=Ready pod -l app.kubernetes.io/instance=cedana -n $CEDANA_NAMESPACE --timeout=300s
    [ "$status" -eq 0 ]

    run validate_propagator_connectivity
    [ "$status" -eq 0 ]
}

# bats test_tags=deploy
@test "Deploy a pod" {
    local name
    name=$(unix_nano)
    local script
    script=$(cat "$WORKLOADS"/date-loop.sh)
    local spec
    spec=$(cmd_pod_spec "$NAMESPACE" "$name" "alpine:latest" "$script")

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=300s -n "$NAMESPACE"
    [ "$status" -eq 0 ]

    run kubectl delete pod "$name" -n "$NAMESPACE" --wait=true
    [ "$status" -eq 0 ]
}

# bats test_tags=dump
@test "Checkpoint a pod (wait for completion, streams=$CEDANA_CHECKPOINT_STREAMS)" {
    local name
    name=$(unix_nano)
    local script
    script=$(cat "$WORKLOADS"/date-loop.sh)
    local spec
    spec=$(cmd_pod_spec "$NAMESPACE" "$name" "alpine:latest" "$script")

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=300s -n "$NAMESPACE"
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
@test "Restore a pod with original pod running (wait until running, streams=$CEDANA_CHECKPOINT_STREAMS)" {
    local name
    name=$(unix_nano)
    local script
    script=$(cat "$WORKLOADS"/date-loop.sh)
    local spec
    spec=$(cmd_pod_spec "$NAMESPACE" "$name" "alpine:latest" "$script")

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=300s -n "$NAMESPACE"
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

        run wait_for_cmd 30 get_restored_pod "$NAMESPACE" "$name"
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
@test "Restore a pod with original pod deleted (wait until running, streams=$CEDANA_CHECKPOINT_STREAMS)" {
    skip # FIXME: Skip until cgroups issue figured out

    local name
    name=$(unix_nano)
    local script
    script=$(cat "$WORKLOADS"/date-loop.sh)
    local spec
    spec=$(cmd_pod_spec "$NAMESPACE" "$name" "alpine:latest" "$script")

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=300s -n "$NAMESPACE"
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

        run wait_for_cmd 30 get_restored_pod "$NAMESPACE" "$name"
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
