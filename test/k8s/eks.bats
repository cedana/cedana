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
    run kubectl wait --for=condition=Ready pod -l app.kubernetes.io/instance=cedana -n $CEDANA_NAMESPACE --timeout=120s
    [ "$status" -eq 0 ]

    run validate_propagator_connectivity
    [ "$status" -eq 0 ]
}

@test "Deploy a pod and verify it's running" {
    name=$(unix_nano)
    spec=/tmp/test-pod-$name.yaml

    cat > "$spec" << EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: $NAMESPACE
  labels:
    app: "$name"
spec:
  restartPolicy: Never
  containers:
  - name: counter
    image: alpine:latest
    command: ["/bin/sh"]
    args: ["-c", "counter=0; while true; do echo \"Count: \$counter\" | tee -a /tmp/counter.log; echo \$counter > /tmp/current_count; counter=\$((counter + 1)); sleep 1; done"]
EOF

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=120s -n "$NAMESPACE"
    [ "$status" -eq 0 ]

    run kubectl delete pod "$name" -n "$NAMESPACE" --wait=false
    [ "$status" -eq 0 ]
}

# bats test_tags=dump
@test "Checkpoint a pod (wait for completion)" {
    name=$(unix_nano)
    spec=/tmp/test-pod-$name.yaml
    local action_id

    cat > "$spec" << EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: $NAMESPACE
  labels:
    app: "$name"
spec:
  restartPolicy: Never
  containers:
  - name: counter
    image: alpine:latest
    command: ["/bin/sh"]
    args: ["-c", "counter=0; while true; do echo \"Count: \$counter\" | tee -a /tmp/counter.log; echo \$counter > /tmp/current_count; counter=\$((counter + 1)); sleep 1; done"]
EOF

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=120s -n "$NAMESPACE"
    [ "$status" -eq 0 ]

    # Checkpoint the test pod
    run checkpoint_pod "$name" "$RUNC_ROOT" "$NAMESPACE" "$CLUSTER_NAME"
    [ "$status" -eq 0 ]

    action_id=$output

    if [ $status -eq 0 ]; then
        run validate_action_id "$action_id"
        [ $status -eq 0 ]

        run poll_action_status "$action_id" "checkpoint"
        [ "$status" -eq 0 ]
    fi

    run kubectl delete pod "$name" -n "$NAMESPACE" --wait=false
    [ "$status" -eq 0 ]
}

# bats test_tags=restore
@test "Restore a pod with original pod running (wait until running)" {
    name=$(unix_nano)
    spec=/tmp/test-pod-$name.yaml
    local action_id

    cat > "$spec" << EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: $NAMESPACE
  labels:
    app: "$name"
spec:
  restartPolicy: Never
  containers:
  - name: counter
    image: alpine:latest
    command: ["/bin/sh"]
    args: ["-c", "counter=0; while true; do echo \"Count: \$counter\" | tee -a /tmp/counter.log; echo \$counter > /tmp/current_count; counter=\$((counter + 1)); sleep 1; done"]
EOF

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=120s -n "$NAMESPACE"
    [ "$status" -eq 0 ]

    # Checkpoint the test pod
    run checkpoint_pod "$name" "$RUNC_ROOT" "$NAMESPACE" "$CLUSTER_NAME"
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

            run kubectl delete pod "$restored_pod" -n "$NAMESPACE" --wait=false
            [ "$status" -eq 0 ]
        fi
    fi

    run kubectl delete pod "$name" -n "$NAMESPACE" --wait=false
    [ "$status" -eq 0 ]
}

# bats test_tags=restore
@test "Restore a pod with original pod deleted (wait until running)" {
    name=$(unix_nano)
    spec=/tmp/test-pod-$name.yaml
    local action_id

    cat > "$spec" << EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: $NAMESPACE
  labels:
    app: "$name"
spec:
  restartPolicy: Never
  containers:
  - name: counter
    image: alpine:latest
    command: ["/bin/sh"]
    args: ["-c", "counter=0; while true; do echo \"Count: \$counter\" | tee -a /tmp/counter.log; echo \$counter > /tmp/current_count; counter=\$((counter + 1)); sleep 1; done"]
EOF

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=120s -n "$NAMESPACE"
    [ "$status" -eq 0 ]

    # Checkpoint the test pod
    run checkpoint_pod "$name" "$RUNC_ROOT" "$NAMESPACE" "$CLUSTER_NAME"
    [ "$status" -eq 0 ]

    action_id=$output

    if [ $status -eq 0 ]; then
        run validate_action_id "$action_id"
        [ $status -eq 0 ]

        run poll_action_status "$action_id" "checkpoint"
        [ "$status" -eq 0 ]
    fi

    run kubectl delete pod "$name" -n "$NAMESPACE" --wait=false
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

            run kubectl delete pod "$restored_pod" -n "$NAMESPACE" --wait=false
            [ "$status" -eq 0 ]
        fi
    fi
}

@test "Deploy a GPU pod and verify it's running" {
    name=$(unix_nano)
    spec=/tmp/test-gpu-pod-$name.yaml

    cat > "$spec" << EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: $NAMESPACE
  labels:
    app: "$name"
spec:
  restartPolicy: Never
  nodeSelector:
    instance-type: gpu
  containers:
  - name: gpu-test
    image: nvidia/cuda:12.4-base-ubuntu24.04
    command: ["/bin/bash"]
    args: ["-c", "nvidia-smi && echo 'GPU test completed'"]
    resources:
      limits:
        nvidia.com/gpu: 1
      requests:
        nvidia.com/gpu: 1
EOF

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=120s -n "$NAMESPACE"
    [ "$status" -eq 0 ]

    # Wait for pod to complete
    run kubectl wait --for=jsonpath='{.status.phase}=Succeeded' pod/"$name" --timeout=300s -n "$NAMESPACE"
    [ "$status" -eq 0 ]

    # Check logs to verify GPU access
    run kubectl logs "$name" -n "$NAMESPACE"
    [ "$status" -eq 0 ]
    [[ "$output" == *"GPU test completed"* ]]

    run kubectl delete pod "$name" -n "$NAMESPACE" --wait=false
    [ "$status" -eq 0 ]
}

@test "Checkpoint a GPU pod" {
    name=$(unix_nano)
    spec=/tmp/test-gpu-pod-$name.yaml

    cat > "$spec" << EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: $NAMESPACE
  labels:
    app: "$name"
spec:
  restartPolicy: Never
  runtimeClassName: cedana # for Cedana GPU C/R support
  nodeSelector:
    instance-type: gpu
  containers:
  - name: gpu-counter
    image: nvidia/cuda:12.4-base-ubuntu24.04
    command: ["/bin/bash"]
    args: ["-c", "counter=0; while true; do echo \"GPU Count: \$counter\" | tee -a /tmp/gpu_counter.log; echo \$counter > /tmp/current_gpu_count; counter=\$((counter + 1)); sleep 1; done"]
    resources:
      limits:
        nvidia.com/gpu: 1
      requests:
        nvidia.com/gpu: 1
EOF

    run kubectl apply -f "$spec"
    [ "$status" -eq 0 ]

    # Check if pod is running
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=120s -n "$NAMESPACE"
    [ "$status" -eq 0 ]

    # Checkpoint the GPU test pod
    run checkpoint_pod "$name" "$RUNC_ROOT" "$NAMESPACE" "$CLUSTER_NAME"
    [ "$status" -eq 0 ]

    if [ $status -eq 0 ]; then
        # Extract UUID pattern from the response
        action_id=$(echo "$output" | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)

        # Validate the action ID is a proper UUID
        if [[ "$action_id" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]; then
            echo "✅ GPU checkpoint initiated with action ID: $action_id"
        else
            echo "Error: Invalid action ID format received from response: $output"
            return 1
        fi
    fi

    run kubectl delete pod "$name" -n "$NAMESPACE" --wait=false
    [ "$status" -eq 0 ]
}
