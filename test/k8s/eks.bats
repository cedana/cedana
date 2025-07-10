#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=k8s,eks

load ../helpers/utils
load ../helpers/daemon
load ../helpers/eks
load ../helpers/helm
load ../helpers/propagator

export CLUSTER_NAME="cedana-ci-nightly"
export NAMESPACE="default"
export RUNC_ROOT="/run/containerd/runc/k8s.io"

setup_file() {
    setup_eks_cluster
    wait_for_eks_node_groups
    install_nvidia_gpu_operator
    verify_gpu_operator
    helm_install_cedana $CLUSTER_NAME
    restart_eks_cluster
    tail_helm_cedana_logs &
}

teardown_file() {
    helm_uninstall_cedana
    teardown_eks_cluster
}

@test "Verify EKS cluster and Cedana installation" {
    # Test that EKS cluster is running
    run kubectl get nodes
    [ "$status" -eq 0 ]
    [[ "$output" == *"Ready"* ]]

    # Test that Cedana components are running
    run kubectl get pods -n cedana-system
    [ "$status" -eq 0 ]

    # Check if all Cedana pods are actually ready
    run kubectl wait --for=condition=Ready pod -l app.kubernetes.io/instance=cedana -n cedana-system --timeout=120s
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

    run kubectl delete pod "$name" -n "$NAMESPACE"
    [ "$status" -eq 0 ]
}

@test "Checkpoint a pod" {
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

    # Checkpoint the test pod
    run checkpoint_pod "$name" "$RUNC_ROOT" "$NAMESPACE" "$CLUSTER_NAME"
    [ "$status" -eq 0 ]

    if [ $status -eq 0 ]; then
        # According to the OpenAPI spec, the checkpoint endpoint returns plain text action_id
        # Extract UUID pattern from the response to handle any potential debug text
        action_id=$(echo "$output" | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)

        # Validate the action ID is a proper UUID
        if [[ "$action_id" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]; then
            echo "✅ Checkpoint initiated with action ID: $action_id"
        else
            echo "Error: Invalid action ID format received from response: $output"
            return 1
        fi
    fi

    kubectl delete -f "$spec"
    [ "$status" -eq 0 ]
}

@test "Checkpoint a pod and wait for completion" {
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

    # Checkpoint the test pod
    run checkpoint_pod "$name" "$RUNC_ROOT" "$NAMESPACE" "$CLUSTER_NAME"
    [ "$status" -eq 0 ]

    if [ $status -eq 0 ]; then
        # According to the OpenAPI spec, the checkpoint endpoint returns plain text action_id
        # Extract UUID pattern from the response to handle any potential debug text
        action_id=$(echo "$output" | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)

        # Validate the action ID is a proper UUID
        if [[ "$action_id" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]; then
            echo "✅ Checkpoint initiated with action ID: $action_id"
        else
            echo "Error: Invalid action ID format received from response: $output"
            return 1
        fi

        echo "Polling checkpoint action status using dedicated endpoint..."
        run poll_action_status "$action_id" "checkpoint"
        [ "$status" -eq 0 ]

        # Get the checkpoint ID using the action ID
        echo "Retrieving checkpoint ID from action..."
        checkpoint_id=$(get_checkpoint_id_from_action "$action_id")

        if [ $? -eq 0 ] && [ -n "$checkpoint_id" ] && [ "$checkpoint_id" != "null" ]; then
            echo "✅ Checkpoint completed with ID: $checkpoint_id"
        else
            echo "Error: Failed to get checkpoint ID for action $action_id"
            return 1
        fi
    fi

    kubectl delete -f "$spec"
    [ "$status" -eq 0 ]
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

    run kubectl delete pod "$name" -n "$NAMESPACE"
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

    kubectl delete -f "$spec"
    [ "$status" -eq 0 ]
} 