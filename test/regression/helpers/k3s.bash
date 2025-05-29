#!/usr/bin/env bash

################################
### K3s and Helm Setup Helpers ###
################################

#
# Set up k3s cluster with Helm support for containerized environment
# Designed to work inside Docker containers for e2e testing
#
setup_k3s_cluster_with_helm() {
    echo "Setting up k3s cluster with Helm support in containerized environment..."
    
    # Download k3s binary directly instead of using installer
    if [ ! -f /usr/local/bin/k3s ]; then
        echo "Downloading k3s binary..."
        curl -Lo /usr/local/bin/k3s https://github.com/k3s-io/k3s/releases/latest/download/k3s
        chmod +x /usr/local/bin/k3s
    fi
    
    # Create necessary directories
    mkdir -p /etc/rancher/k3s
    mkdir -p /var/lib/rancher/k3s
    
    # Start k3s server directly in background
    echo "Starting k3s server..."
    nohup k3s server \
        --disable=traefik \
        --disable=servicelb \
        --disable=metrics-server \
        --write-kubeconfig-mode=644 \
        --data-dir=/var/lib/rancher/k3s \
        > /tmp/k3s.log 2>&1 &
    
    K3S_PID=$!
    echo "k3s server started with PID: $K3S_PID"
    
    # Wait for kubeconfig to be created
    echo "Waiting for kubeconfig..."
    for i in $(seq 1 60); do
        if [ -f /etc/rancher/k3s/k3s.yaml ]; then
            echo "Kubeconfig found, setting permissions..."
            chmod 644 /etc/rancher/k3s/k3s.yaml
            break
        fi
        echo "Waiting for kubeconfig (attempt $i/60)..."
        sleep 2
    done
    
    if [ ! -f /etc/rancher/k3s/k3s.yaml ]; then
        echo "Error: Kubeconfig not created after 120 seconds"
        echo "k3s log output:"
        tail -20 /tmp/k3s.log || echo "No log file found"
        return 1
    fi
    
    # Set up kubeconfig
    export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
    
    # Wait for k3s to be ready
    wait_for_k3s_ready
    
    echo "k3s cluster with Helm support is ready."
    return 0
}

#
# Wait for k3s cluster to be ready
#
wait_for_k3s_ready() {
    echo "Waiting for k3s cluster to become ready..."
    
    for i in $(seq 1 60); do
        if kubectl get nodes 2>/dev/null | grep -q 'Ready'; then
            echo "k3s cluster is ready."
            return 0
        fi
        echo "Waiting for k3s (attempt $i/60)..."
        sleep 5
    done
    
    echo "Error: Timed out waiting for k3s cluster."
    kubectl get nodes 2>/dev/null || echo "kubectl not accessible"
    return 1
}

#
# Configure k3s runc root path for container environment
#
configure_k3s_runc_root() {
    echo "Configuring k3s runc root path..."
    
    # Ensure the runc root directory exists
    mkdir -p /run/containerd/runc/k8s.io
    
    # Set proper permissions
    chmod 755 /run/containerd/runc/k8s.io
    
    echo "k3s runc root configured at /run/containerd/runc/k8s.io"
    return 0
}

#
# Deploy Cedana Helm chart using OCI registry
# @param $1: Auth token for Cedana
# @param $2: Cedana URL (default: ci.cedana.ai/v1)
#
deploy_cedana_helm_chart() {
    local auth_token="$1"
    local cedana_url="${2:-ci.cedana.ai/v1}"
    
    if [ -z "$auth_token" ]; then
        echo "Error: Auth token required for Cedana Helm chart deployment"
        return 1
    fi
    
    echo "Deploying Cedana Helm chart from OCI registry..."
    
    # Install Cedana using OCI registry
    helm install cedana oci://registry-1.docker.io/cedana/cedana-helm \
        --create-namespace -n cedanacontroller-system \
        --set cedanaConfig.cedanaAuthToken="$auth_token" \
        --set cedanaConfig.cedanaUrl="$cedana_url" \
        --set controllerManager.manager.resources.limits.cpu=200m \
        --set controllerManager.manager.resources.limits.memory=128Mi \
        --set controllerManager.manager.resources.requests.cpu=100m \
        --set controllerManager.manager.resources.requests.memory=64Mi \
        --wait --timeout=10m
    
    if [ $? -ne 0 ]; then
        echo "Error: Failed to deploy Cedana Helm chart"
        kubectl get pods -n cedanacontroller-system 2>/dev/null || true
        return 1
    fi
    
    echo "Cedana Helm chart deployed successfully."
    return 0
}

#
# Teardown k3s cluster completely
#
teardown_k3s_cluster() {
    echo "Tearing down k3s cluster..."
    
    # Uninstall Cedana helm release if it exists
    if [ -f /etc/rancher/k3s/k3s.yaml ]; then
        export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
        helm uninstall cedana -n cedanacontroller-system --wait 2>/dev/null || true
    fi
    
    # Stop k3s processes
    pkill -f "k3s server" 2>/dev/null || true
    pkill -f "k3s agent" 2>/dev/null || true
    
    # Wait for processes to stop
    sleep 5
    
    # Uninstall k3s
    if [ -f /usr/local/bin/k3s-uninstall.sh ]; then
        /usr/local/bin/k3s-uninstall.sh 2>/dev/null || true
    fi
    
    # Clean up any remaining artifacts
    rm -rf /etc/rancher/k3s /var/lib/rancher/k3s /run/k3s /tmp/k3s.log 2>/dev/null || true
    
    echo "k3s cluster teardown complete."
    return 0
}

#
# Assumes 'curl' and 'helm' are installed.
#
# @param $1: Path to the Cedana helm chart (e.g., ./cedana-helm)
# @param $2: Cedana API URL (e.g., https://sandbox.cedana.ai)
# @param $3: Cedana Auth Token
# @param $4: cedana-helper image tag
# @param $5: cedana-controller image tag
#
setup_k3s_and_install_helm_chart() {
    # 1. Parameter validation
    local chart_path="$1"
    local cedana_url="$2"
    local auth_token="$3"
    local helper_tag="$4"
    local controller_tag="$5"

    if [ -z "$chart_path" ] || [ -z "$cedana_url" ] || [ -z "$auth_token" ] || [ -z "$helper_tag" ] || [ -z "$controller_tag" ]; then
        echo "Usage: setup_k3s_and_install_helm_chart <chart_path> <cedana_url> <auth_token> <helper_tag> <controller_tag>"
        return 1
    fi

    if ! command -v curl &>/dev/null || ! command -v helm &>/dev/null; then
        echo "Error: 'curl' and 'helm' must be installed to proceed."
        return 1
    fi

    echo "Installing k3s..."
    if [ ! -f /usr/local/bin/k3s ]; then
        curl -sfL https://get.k3s.io | sudo sh -s -
        if [ $? -ne 0 ]; then
            echo "Error: Failed to install k3s."
            return 1
        fi
    else
        echo "k3s is already installed. Skipping installation."
    fi

    export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
    if [ ! -f "$KUBECONFIG" ]; then
        echo "Error: Kubeconfig not found at $KUBECONFIG"
        return 1
    fi
    sudo chmod 644 "$KUBECONFIG"

    # 4. Wait for k3s node to be ready
    echo "Waiting for k3s node to become ready..."
    # Ensure kubectl commands are run with sudo if KUBECONFIG is root-owned
    if ! sudo KUBECONFIG="$KUBECONFIG" kubectl get nodes | grep -q 'Ready'; then
        for _ in $(seq 1 30); do
            if sudo KUBECONFIG="$KUBECONFIG" kubectl get nodes | grep -q ' Ready'; then
                echo "k3s node is ready."
                break
            fi
            sleep 5
        done
    fi
    if ! sudo KUBECONFIG="$KUBECONFIG" kubectl get nodes | grep -q ' Ready'; then
        echo "Error: Timed out waiting for k3s node."
        sudo KUBECONFIG="$KUBECONFIG" kubectl get nodes
        return 1
    fi

    # 5. Install the Helm chart
    echo "Installing Cedana helm chart from '$chart_path'..."
    sudo KUBECONFIG="$KUBECONFIG" helm install cedana "$chart_path" \
        --create-namespace -n cedana-systems \
        --set daemonHelper.image.repository=cedana/cedana-helper-test \
        --set daemonHelper.image.tag="$helper_tag" \
        --set controllerManager.manager.image.repository=cedana/cedana-controller-test \
        --set controllerManager.manager.image.tag="$controller_tag" \
        --set cedanaConfig.cedanaUrl="$cedana_url" \
        --set cedanaConfig.cedanaAuthToken="$auth_token"

    if [ $? -ne 0 ]; then
        echo "Error: Helm install failed."
        return 1
    fi

    # 6. Wait for deployments to be ready
    echo "Waiting for Cedana deployments to become available..."
    sudo KUBECONFIG="$KUBECONFIG" kubectl wait --for=condition=Available=True deployment \
        -l app.kubernetes.io/instance=cedana \
        -n cedana-systems \
        --timeout=300s

    if [ $? -ne 0 ]; then
        echo "Error: Timed out waiting for Cedana deployments to become available."
        sudo KUBECONFIG="$KUBECONFIG" kubectl get pods -n cedana-systems
        return 1
    fi

    echo "Cedana helm chart installed and deployments are ready."
    return 0
}

teardown_k3s() {
    echo "Uninstalling Cedana helm release..."
    if [ -f /etc/rancher/k3s/k3s.yaml ]; then
        export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
        sudo helm uninstall cedana -n cedana-systems --wait 2>/dev/null || true
    fi

    echo "Uninstalling k3s..."
    if [ -f /usr/local/bin/k3s-uninstall.sh ]; then
        sudo /usr/local/bin/k3s-uninstall.sh
    fi

    echo "k3s teardown complete."
    return 0
}

##################################
### Pod Manifest Generation    ###
##################################

#
# Generates a Kubernetes Pod YAML manifest.
#
# @param $1: Pod name (e.g., "my-test-pod")
# @param $2: Container image (e.g., "myregistry/myimage:latest")
# @param $3: Container command as a single string (e.g., "/app/workload")
# @param $4: Number of GPUs to request (e.g., 1 for GPU, 0 for CPU-only)
#
generate_pod_manifest() {
    local pod_name="$1"
    local image_name="$2"
    local container_command="$3"
    local gpu_count="$4"

    if [ -z "$pod_name" ] || [ -z "$image_name" ] || [ -z "$container_command" ] || [ -z "$gpu_count" ]; then
        echo "Usage: generate_pod_manifest <pod_name> <image_name> <container_command> <gpu_count>" >&2
        return 1
    fi

    local resources_yaml=""
    if [ "$gpu_count" -gt 0 ]; then
        resources_yaml=$(
            cat <<EOF
      resources:
        limits:
          nvidia.com/gpu: $gpu_count
EOF
        )
    fi

    cat <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
  labels:
    app: cedana-test # Label for easier cleanup
spec:
  restartPolicy: Never # Important for C/R tests
  containers:
  - name: workload-container
    image: "$image_name"
    command: ["/bin/sh", "-c", "$container_command"]
$resources_yaml
EOF
}

###########################
### Cedana API Wrappers ###
###########################

# Generic function to make a GET request to the Cedana API
#
# $1: API path (e.g., "/v2/pods")
#
function api_get() {
    local path="$1"
    curl --silent --show-error \
        -H "Authorization: Bearer $CEDANA_API_KEY" \
        "$CEDANA_API_URL$path"
}

# Generic function to make a POST request to the Cedana API
#
# $1: API path (e.g., "/v2/checkpoint/pod")
# $2: JSON payload
#
function api_post() {
    local path="$1"
    local data="$2"
    curl --silent --show-error \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $CEDANA_API_KEY" \
        -X POST \
        -d "$data" \
        "$CEDANA_API_URL$path"
}

# Start a checkpoint action for a given pod
#
# $1: Pod name
# $2: Namespace
#
function checkpoint_pod() {
    local pod_name="$1"
    local namespace="$2"

    local data
    data=$(jq -n \
        --arg pod_name "$pod_name" \
        --arg namespace "$namespace" \
        '{
            "pod_name": $pod_name,
            "namespace": $namespace,
            "cluster_id": "default",
            "runc_root": "/run/containerd/runc/k8s.io"
        }')

    api_post "/v2/checkpoint/pod" "$data"
}

# Start a restore action from a given checkpoint ID
#
# $1: Checkpoint ID (from a completed checkpoint action)
#
function restore_pod() {
    local checkpoint_id="$1"

    local data
    data=$(jq -n --arg action_id "$checkpoint_id" '{action_id: $action_id}')

    api_post "/v2/restore/pod" "$data"
}

# Wait for a pod to enter the 'Running' state
#
# $1: Pod name
#
function wait_for_pod_running() {
    local pod_name="$1"
    echo "Waiting for pod '$pod_name' to be running..."
    for _ in $(# Increased timeout for potentially slower CI environments
        seq 1 30
    ); do
        # Use sudo -E to preserve KUBECONFIG if set and needed
        local status
        status=$(sudo -E KUBECONFIG="$KUBECONFIG" kubectl get pod "$pod_name" -n default -o jsonpath='{.status.phase}' 2>/dev/null)
        if [[ "$status" == "Running" ]]; then
            echo "Pod '$pod_name' is running."
            return 0
        elif [[ "$status" == "Succeeded" || "$status" == "Failed" ]]; then
            echo "Error: Pod '$pod_name' exited prematurely with status '$status'."
            sudo -E KUBECONFIG="$KUBECONFIG" kubectl logs "$pod_name" -n default --tail=50
            return 1
        fi
        sleep 5 # Increased sleep interval
    done
    echo "Error: Timed out waiting for pod '$pod_name' to run."
    sudo -E KUBECONFIG="$KUBECONFIG" kubectl describe pod "$pod_name" -n default
    return 1
}

# Wait for a checkpoint or restore action to complete
#
# $1: Action ID
#
function wait_for_action_complete() {
    local action_id="$1"
    echo "Waiting for action '$action_id' to complete..."
    for _ in $(# 3 minutes timeout
        seq 1 60
    ); do
        local action_info
        action_info=$(api_get "/v2/actions" | jq --arg id "$action_id" '.[] | select(.action_id == $id)')

        local status
        status=$(echo "$action_info" | jq -r '.status')

        if [[ "$status" == "completed" || "$status" == "ready" ]]; then # 'ready' is often used for checkpoints
            echo "Action '$action_id' completed successfully with status '$status'."
            # Return the full action JSON so the caller can extract the checkpoint_id
            echo "$action_info"
            return 0
        elif [[ "$status" == "failed" || "$status" == "error" ]]; then
            echo "Error: Action '$action_id' failed with status '$status'."
            echo "Action info: $action_info"
            return 1
        fi
        sleep 3
    done
    echo "Error: Timed out waiting for action '$action_id' to complete."
    echo "Last known action info: $(api_get "/v2/actions" | jq --arg id "$action_id" '.[] | select(.action_id == $id)')"
    return 1
}
