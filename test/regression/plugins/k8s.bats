#!/usr/bin/env bats

load ../helpers/utils
load ./k8s.bash # Load the k8s helpers

load_lib support
load_lib assert
load_lib file

# Define the test pod manifest
TEST_POD_MANIFEST="test-pod.yaml"

# Setup runs once before all tests in this file
setup_file() {
   # --- Set up k3s cluster and install Helm chart ---
    # This function is defined in k8s.bash
    run setup_k3s_and_install_helm_chart \
        "$CEDANA_CHART_PATH" \
        "$CEDANA_API_URL" \
        "$CEDANA_API_KEY" \
        "$CEDANA_HELPER_TAG" \
        "$CEDANA_CONTROLLER_TAG"
    assert_success "k3s and Helm chart setup should succeed"

    # Export the KUBECONFIG path for subsequent commands in this script
    export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
}

# Teardown runs once after all tests in this file
teardown_file() {
    echo "Tearing down k3s cluster..."
    # This function is defined in k8s.bash
    run teardown_k3s
    assert_success "k3s teardown should succeed"
}

# Teardown runs after each test
teardown() {
    # Dump logs on failure
    if [ "$status" -ne 0 ]; {
        echo "--- Test failed. Dumping kubectl logs ---"
        # Use sudo -E to preserve the KUBECONFIG env var
        run sudo -E kubectl get pods -A -o wide
        run sudo -E kubectl describe pod vector-add-pod --ignore-not-found=true
        echo "----------------------------------------"
    }
    # Clean up the pod using the manifest
    run sudo -E kubectl delete -f "$TEST_POD_MANIFEST" --ignore-not-found=true
}

###############################
### Checkpoint & Restore Test ###
###############################

@test "checkpoint and restore a GPU pod" {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available on test runner"
    fi

    ### 1. Deploy and verify the initial pod ###
    # Use 'sudo -E' to run kubectl with root privileges while preserving the
    # KUBECONFIG environment variable we exported in setup_file.
    run sudo -E kubectl apply -f "$TEST_POD_MANIFEST"
    assert_success
    assert_output --partial "pod/vector-add-pod created"

    # The helper function must also be run with sudo to interact with the cluster
    run sudo -E wait_for_pod_running "vector-add-pod"
    assert_success

    ### 2. Checkpoint the running pod ###
    run checkpoint_pod "vector-add-pod" "default"
    assert_success
    checkpoint_action_id=$(echo "$output")

    run wait_for_action_complete "$checkpoint_action_id"
    assert_success
    completed_action_info="$output"

    # Extract the checkpoint ID to use for restore
    checkpoint_id=$(echo "$completed_action_info" | jq -r '.checkpoint_id')
    assert_not_equal "$checkpoint_id" "null"

    ### 3. Delete the original pod ###
    run sudo -E kubectl delete pod "vector-add-pod"
    assert_success
    assert_output --partial "pod \"vector-add-pod\" deleted"
    # Wait a moment to ensure it's gone
    sleep 5

    ### 4. Restore the pod from the checkpoint ###
    run restore_pod "$checkpoint_id"
    assert_success
    restore_action_id=$(echo "$output")

    run wait_for_action_complete "$restore_action_id"
    assert_success

    ### 5. Verify the restored pod ###
    # We assume the restored pod has the same name.
    run sudo -E wait_for_pod_running "vector-add-pod"
    assert_success

    run sudo -E kubectl get pod "vector-add-pod"
    assert_success
    echo "Successfully restored pod and confirmed it is running."
}
