#!/usr/bin/env bats

#
# k8s.bats - Tests for Kubernetes checkpoint/restore
#
# This test suite will create and destroy a k3s cluster on the host machine.
# It requires passwordless sudo access to run.
#
# bats file_tags=k8s,gpu

load ../helpers/utils
load ./k8s.bash # Load the k8s helpers

load_lib support
load_lib assert
load_lib file

# Temporary file for the dynamically generated pod manifest
TEMP_POD_MANIFEST=""

# Setup runs once before all tests in this file
setup_file() {
    # --- Check for required environment variables ---
    local required_vars=(
        "CEDANA_CHART_PATH"
        "CEDANA_API_URL"
        "CEDANA_API_KEY"
        "CEDANA_HELPER_TAG"
        "CEDANA_CONTROLLER_TAG"
        "CEDANA_TEST_IMAGE" # e.g., your-registry/your-cedana-samples-image:latest
        "CEDANA_GPU_WORKLOAD_COMMAND" # e.g., "/app/gpu_smr/vector_add; sleep 3600"
    )
    for var in "${required_vars[@]}"; do
        if [ -z "${!var}" ]; then
            skip "Required environment variable '$var' is not set."
        fi
    done

    if ! cmd_exists sudo; then
        skip "sudo command not found, cannot manage k3s cluster."
    fi
    if ! cmd_exists jq; then
        skip "jq command not found, required for API interactions."
    fi


    # --- Set up k3s cluster and install Helm chart ---
    run setup_k3s_and_install_helm_chart \
        "$CEDANA_CHART_PATH" \
        "$CEDANA_API_URL" \
        "$CEDANA_API_KEY" \
        "$CEDANA_HELPER_TAG" \
        "$CEDANA_CONTROLLER_TAG"
    assert_success "k3s and Helm chart setup should succeed"

    # Export the KUBECONFIG path for subsequent commands in this script
    # This is critical for sudo -E to pick it up.
    export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
}

# Teardown runs once after all tests in this file
teardown_file() {
    echo "Tearing down k3s cluster..."
    run teardown_k3s
    assert_success "k3s teardown should succeed"
}

# Setup before each test
setup() {
    # Create a temporary file for the pod manifest for this test
    TEMP_POD_MANIFEST=$(mktemp --suffix=.yaml)
    # Ensure KUBECONFIG is available for helper functions if they call kubectl
    export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
}

# Teardown runs after each test
teardown() {
    # Dump logs on failure
    if [ "$status" -ne 0 ]; then
        echo "--- Test failed. Dumping kubectl logs ---"
        # Use sudo -E to preserve the KUBECONFIG env var
        # The pod name might vary if tests generate different names
        # For now, assuming 'vector-add-pod' or the test-specific name
        local pod_to_describe="vector-add-pod" # Default, can be overridden in test
        run sudo -E KUBECONFIG="$KUBECONFIG" kubectl get pods -A -o wide
        run sudo -E KUBECONFIG="$KUBECONFIG" kubectl describe pod "$pod_to_describe" -n default --ignore-not-found=true
        echo "----------------------------------------"
    fi
    # Clean up the pod using the dynamically generated manifest if it exists
    if [ -n "$TEMP_POD_MANIFEST" ] && [ -f "$TEMP_POD_MANIFEST" ]; then
        run sudo -E KUBECONFIG="$KUBECONFIG" kubectl delete -f "$TEMP_POD_MANIFEST" --ignore-not-found=true
        rm -f "$TEMP_POD_MANIFEST"
    fi
    TEMP_POD_MANIFEST=""
}

###############################
### Checkpoint & Restore Test ###
###############################

@test "checkpoint and restore CPU workload" {
    local pod_name="test-pod-$(date +%s)"

    echo "Generating pod manifest for $pod_name..."
    run generate_pod_manifest "$pod_name" "$CEDANA_TEST_IMAGE" "$CEDANA_GPU_WORKLOAD_COMMAND" "1"
    assert_success "Pod manifest generation should succeed"
    echo "$output" > "$TEMP_POD_MANIFEST"
    cat "$TEMP_POD_MANIFEST" # Log the generated manifest

    echo "Applying pod manifest $TEMP_POD_MANIFEST..."
    run sudo -E KUBECONFIG="$KUBECONFIG" kubectl apply -f "$TEMP_POD_MANIFEST"
    assert_success
    assert_output --partial "pod/$pod_name created"

    run sudo -E wait_for_pod_running "$pod_name" # wait_for_pod_running uses KUBECONFIG internally
    assert_success

    ### 2. Checkpoint the running pod ###
    echo "Checkpointing pod $pod_name..."
    run checkpoint_pod "$pod_name" "default" # Assuming 'default' namespace
    assert_success
    checkpoint_action_id=$(echo "$output")
    assert_not_equal "$checkpoint_action_id" "" "Checkpoint action ID should not be empty"


    echo "Waiting for checkpoint action $checkpoint_action_id to complete..."
    run wait_for_action_complete "$checkpoint_action_id"
    assert_success
    completed_action_info="$output"

    # Extract the checkpoint ID to use for restore
    checkpoint_id=$(echo "$completed_action_info" | jq -r '.checkpoint_id')
    assert_not_equal "$checkpoint_id" "null" "Checkpoint ID should not be null"
    assert_not_equal "$checkpoint_id" "" "Checkpoint ID should not be empty"


    ### 3. Delete the original pod ###
    echo "Deleting original pod $pod_name..."
    run sudo -E KUBECONFIG="$KUBECONFIG" kubectl delete pod "$pod_name" -n default
    assert_success
    assert_output --partial "pod \"$pod_name\" deleted"
    # Wait a moment to ensure it's gone and resources are freed
    sleep 10

    ### 4. Restore the pod from the checkpoint ###
    echo "Restoring pod from checkpoint ID $checkpoint_id..."
    run restore_pod "$checkpoint_id"
    assert_success
    restore_action_id=$(echo "$output")
    assert_not_equal "$restore_action_id" "" "Restore action ID should not be empty"

    echo "Waiting for restore action $restore_action_id to complete..."
    run wait_for_action_complete "$restore_action_id"
    assert_success

    ### 5. Verify the restored pod ###
    # The restored pod should have the same name as the original.
    echo "Verifying restored pod $pod_name..."
    run sudo -E wait_for_pod_running "$pod_name"
    assert_success

    run sudo -E KUBECONFIG="$KUBECONFIG" kubectl get pod "$pod_name" -n default
    assert_success
    echo "Successfully restored pod '$pod_name' and confirmed it is running."
}
