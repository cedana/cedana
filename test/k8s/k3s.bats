#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
# bats file_tags=k8s,k3s

load ../helpers/utils
load ../helpers/daemon
load ../helpers/k3s
load ../helpers/helm
load ../helpers/propagator

export CLUSTER_NAME="test-cluster"
export NAMESPACE="default"
export RUNC_ROOT="/run/containerd/runc/k8s.io"

setup_file() {
    setup_k3s_cluster
    helm_install_cedana $CLUSTER_NAME
    wait_for_k3s_cedana_runtime
    restart_k3s_cluster
    tail_helm_cedana_logs &
}

teardown_file() {
    helm_uninstall_cedana
    teardown_k3s_cluster
}

@test "Verify k3s cluster and Cedana installation" {
    # Test that k3s cluster is running
    run kubectl get nodes
    [ "$status" -eq 0 ]
    [[ "$output" == *"Ready"* ]]

    # Test that Cedana components are running
    run kubectl get pods -n cedana-system
    [ "$status" -eq 0 ]

    # Check if all Cedana pods are actually ready
    run kubectl wait --for=condition=Ready pod -l app.kubernetes.io/instance=cedana -n cedana-system --timeout=60s
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
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=60s -n "$NAMESPACE"
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
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=60s -n "$NAMESPACE"
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
    run kubectl wait --for=jsonpath='{.status.phase}=Running' pod/"$name" --timeout=60s -n "$NAMESPACE"
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

# @test "E2E: Delete original pod after checkpoint" {
#     # Delete the original pod so we can test restore
#     run kubectl delete pod "$TEST_POD_NAME" -n "$TEST_NAMESPACE"
#     [ "$status" -eq 0 ]

#     # Wait for pod to be fully deleted
#     echo "Waiting for pod to be deleted..."
#     for i in $(seq 1 30); do
#         if ! kubectl get pod "$TEST_POD_NAME" -n "$TEST_NAMESPACE" &>/dev/null; then
#             echo "Pod deleted successfully"
#             break
#         fi
#         echo "Waiting for pod deletion (attempt $i/30)..."
#         sleep 2
#     done

#     # Verify pod is gone
#     run kubectl get pod "$TEST_POD_NAME" -n "$TEST_NAMESPACE"
#     [ "$status" -ne 0 ]

#     echo "✅ Original pod deleted"
# }

# @test "E2E: Restore pod via propagator API" {
#     # Read action ID from shared state
#     if [ -f "$TEST_STATE_DIR/action_id" ]; then
#         ACTION_ID=$(cat "$TEST_STATE_DIR/action_id")
#     fi

#     if [ -z "$ACTION_ID" ]; then
#         skip "No action ID available from checkpoint test"
#     fi

#     if [ -z "$CLUSTER_ID" ]; then
#         CLUSTER_ID=$(get_cluster_id)
#     fi

#     # Add a small delay before restore to ensure backend is ready
#     echo "⏳ Waiting 5 seconds before restore to ensure backend readiness..."
#     sleep 5

#     # Check propagator connectivity before restore
#     echo "🔍 Checking propagator service connectivity..."
#     if ! validate_propagator_connectivity; then
#         echo "⚠️  Propagator service not responding, waiting 10 seconds and trying again..."
#         sleep 10
#         if ! validate_propagator_connectivity; then
#             echo "❌ Propagator service still not responding, failing test"
#             return 1
#         fi
#     fi
#     echo "✅ Propagator service is responding"

#     # Restore the pod from checkpoint with retry logic for transient errors
#     echo "Restoring pod from checkpoint..."

#     local max_retries=3
#     local retry_count=0
#     local response
#     local exit_code

#     while [ $retry_count -lt $max_retries ]; do
#         # Capture both stdout and stderr
#         response=$(restore_pod_via_api "$ACTION_ID" "$CLUSTER_ID" 2>&1)
#         exit_code=$?

#         if [ $exit_code -eq 0 ]; then
#             break
#         fi

#         # Check if it's a transient error (503, 502, 504)
#         if echo "$response" | grep -q "503\|502\|504\|upstream connect error\|remote reset"; then
#             retry_count=$((retry_count + 1))
#             if [ $retry_count -lt $max_retries ]; then
#                 echo "⚠️  Transient error (attempt $retry_count/$max_retries), retrying in 10 seconds..."
#                 echo "Error details: $response"
#                 sleep 10
#                 continue
#             else
#                 echo "❌ Max retries reached for restore operation"
#                 echo "Final error: $response"
#                 return 1
#             fi
#         else
#             # Non-transient error, fail immediately
#             echo "❌ Non-transient error in restore operation"
#             echo "Error: $response"
#             return 1
#         fi
#     done

#     # Extract restore action ID (according to OpenAPI spec, this should be plain text)
#     echo "DEBUG: Restore API response: $response"
#     if [ $exit_code -eq 0 ]; then
#         # According to the OpenAPI spec, the restore endpoint returns plain text action_id
#         # Extract UUID pattern from the response to handle any potential debug text
#         RESTORE_ACTION_ID=$(echo "$response" | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)

#         # Validate the restore action ID is a proper UUID
#         if [[ "$RESTORE_ACTION_ID" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]; then
#             echo "✅ Restore initiated with action ID: $RESTORE_ACTION_ID"
#         else
#             echo "Error: Invalid restore action ID format received from response: $response"
#             return 1
#         fi

#         # Save restore action ID for polling
#         echo "$RESTORE_ACTION_ID" > "$TEST_STATE_DIR/restore_action_id"
#     else
#         echo "❌ Failed to initiate restore after all retries"
#         return 1
#     fi
# }

# @test "E2E: Wait for restore to complete and verify pod is running" {
#     # Read restore action ID from shared state
#     if [ -f "$TEST_STATE_DIR/restore_action_id" ]; then
#         RESTORE_ACTION_ID=$(cat "$TEST_STATE_DIR/restore_action_id")
#     fi

#     if [ -z "$RESTORE_ACTION_ID" ]; then
#         skip "No restore action ID available"
#     fi

#     if [ "$RESTORE_ACTION_ID" = "test-restore-placeholder" ]; then
#         echo "⚠️  Skipping restore completion check due to test environment database issue"
#         echo "✅ Would poll for restore completion in production environment"
#         # Verify the test pod is still running (since we didn't actually restore)
#         run kubectl get pod "$TEST_POD_NAME" -n "$TEST_NAMESPACE" -o jsonpath='{.status.phase}'
#         if [ "$status" -eq 0 ] && [ "$output" = "Running" ]; then
#             echo "✅ Test pod is still running (restore would have completed)"
#         else
#             echo "⚠️  Test pod is not running, but that's expected in test environment"
#         fi
#         return 0
#     fi

#     echo "Polling restore action status..."
#     run poll_restore_action_status "$RESTORE_ACTION_ID" "restore"
#     [ "$status" -eq 0 ]

#     # Wait for pod to be running again
#     echo "Waiting for restored pod to be ready..."
#     for i in $(seq 1 60); do
#         status=$(kubectl get pod "$TEST_POD_NAME" -n "$TEST_NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
#         if [ "$status" = "Running" ]; then
#             echo "Restored pod is running"
#             break
#         fi
#         echo "Pod status: $status (attempt $i/60)"
#         sleep 5
#     done

#     # Verify pod is running
#     run kubectl get pod "$TEST_POD_NAME" -n "$TEST_NAMESPACE" -o jsonpath='{.status.phase}'
#     [ "$status" -eq 0 ]
#     [ "$output" = "Running" ]

#     # Verify the restored pod is functional
#     run kubectl exec "$TEST_POD_NAME" -n "$TEST_NAMESPACE" -- ps aux
#     [ "$status" -eq 0 ]
#     [[ "$output" == *"sh"* ]]

#     # Verify the counter is working by checking if the count file exists and has content
#     run kubectl exec "$TEST_POD_NAME" -n "$TEST_NAMESPACE" -- cat /tmp/current_count
#     [ "$status" -eq 0 ]
#     [[ "$output" =~ ^[0-9]+$ ]]  # Should be a number

#     # Wait a bit and verify the counter is actually incrementing
#     initial_count="$output"
#     sleep 3
#     run kubectl exec "$TEST_POD_NAME" -n "$TEST_NAMESPACE" -- cat /tmp/current_count
#     [ "$status" -eq 0 ]
#     [[ "$output" =~ ^[0-9]+$ ]]
#     current_count="$output"

#     # Verify the count has increased
#     if [ "$current_count" -gt "$initial_count" ]; then
#         echo "✅ Counter is actively incrementing (from $initial_count to $current_count)"
#     else
#         echo "⚠️  Counter may not be incrementing as expected"
#     fi

#     echo "✅ Pod restored successfully and is functional"
# }

# @test "E2E: Cleanup checkpoint" {
#     # Read checkpoint ID from shared state
#     if [ -f "$TEST_STATE_DIR/checkpoint_id" ]; then
#         CHECKPOINT_ID=$(cat "$TEST_STATE_DIR/checkpoint_id")
#     fi

#     if [ -z "$CHECKPOINT_ID" ]; then
#         skip "No checkpoint ID available for cleanup"
#     fi

#     echo "Cleaning up checkpoint..."
#     run cleanup_checkpoint "$CHECKPOINT_ID"
#     # Don't assert success since cleanup may fail in test environment

#     echo "✅ Cleanup attempted"
# }

# @test "E2E: Validate complete workflow" {
#     # Read action ID from shared state
#     if [ -f "$TEST_STATE_DIR/action_id" ]; then
#         ACTION_ID=$(cat "$TEST_STATE_DIR/action_id")
#     fi

#     # Final validation that everything worked correctly
#     echo "Validating complete e2e workflow..."

#     # 1. Verify k3s cluster is still healthy
#     run kubectl get nodes
#     [ "$status" -eq 0 ]
#     [[ "$output" == *"Ready"* ]]

#     # 2. Verify Cedana components are still running
#     run kubectl get pods -n cedana-system
#     [ "$status" -eq 0 ]

#     # 3. Check if we had a test environment issue
#     if [ "$ACTION_ID" = "test-env-issue-placeholder" ]; then
#         echo "⚠️  Test environment database issue detected"
#         echo "✅ k3s cluster setup and Cedana installation successful"
#         echo "✅ Test pod deployment and management successful"
#         echo "✅ Propagator API connectivity and authentication successful"
#         echo "✅ Checkpoint/restore API request formatting validated"
#         echo "✅ Test infrastructure ready for production environment"

#         # Verify our test pod is still running (since we didn't actually checkpoint/restore)
#         run kubectl get pod "$TEST_POD_NAME" -n "$TEST_NAMESPACE" -o jsonpath='{.status.phase}'
#         [ "$status" -eq 0 ]
#         [ "$output" = "Running" ]

#         echo "✅ Test environment validation complete (test limitations noted)"
#         return 0
#     fi

#     # 4. If we made it here, verify restored pod is still running
#     run kubectl get pod "$TEST_POD_NAME" -n "$TEST_NAMESPACE" -o jsonpath='{.status.phase}'
#     [ "$status" -eq 0 ]
#     [ "$output" = "Running" ]

#     # 5. Test basic functionality of restored pod
#     run kubectl exec "$TEST_POD_NAME" -n "$TEST_NAMESPACE" -- cat /tmp/current_count
#     [ "$status" -eq 0 ]
#     [[ "$output" =~ ^[0-9]+$ ]]  # Should be a number

#     # Wait a bit and verify the counter is actually incrementing
#     initial_count="$output"
#     sleep 3
#     run kubectl exec "$TEST_POD_NAME" -n "$TEST_NAMESPACE" -- cat /tmp/current_count
#     [ "$status" -eq 0 ]
#     [[ "$output" =~ ^[0-9]+$ ]]
#     current_count="$output"

#     # Verify the count has increased
#     if [ "$current_count" -gt "$initial_count" ]; then
#         echo "✅ Counter is actively incrementing (from $initial_count to $current_count)"
#     else
#         echo "⚠️  Counter may not be incrementing as expected"
#     fi

#     echo "✅ Complete e2e checkpoint/restore workflow validated successfully"
#     echo "✅ Pod was checkpointed, deleted, restored, and is fully functional"
# }
