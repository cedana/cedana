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
    
    # Wait for containerd to be ready (started by entrypoint)
    echo "Waiting for containerd to be ready..."
    for i in $(seq 1 30); do
        if ctr version >/dev/null 2>&1; then
            echo "Containerd is ready"
            break
        fi
        echo "Waiting for containerd (attempt $i/30)..."
        sleep 2
    done
    
    # Start k3s server directly in background with container-friendly settings
    echo "Starting k3s server..."
    nohup k3s server \
        --disable=traefik \
        --disable=servicelb \
        --disable=metrics-server \
        --write-kubeconfig-mode=644 \
        --data-dir=/var/lib/rancher/k3s \
        --snapshotter=native \
        --rootless=false \
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
    
    # Build helm install command
    local helm_cmd="helm install cedana oci://registry-1.docker.io/cedana/cedana-helm"
    helm_cmd="$helm_cmd --create-namespace -n cedana-systems"
    helm_cmd="$helm_cmd --set cedanaConfig.cedanaAuthToken=\"$auth_token\""
    helm_cmd="$helm_cmd --set cedanaConfig.cedanaUrl=\"$cedana_url\""
    helm_cmd="$helm_cmd --set cedanaConfig.cedanaClusterName=\"ci-k3s-test-cluster\""
    helm_cmd="$helm_cmd --set controllerManager.manager.resources.limits.cpu=200m"
    helm_cmd="$helm_cmd --set controllerManager.manager.resources.limits.memory=128Mi"
    helm_cmd="$helm_cmd --set controllerManager.manager.resources.requests.cpu=100m"
    helm_cmd="$helm_cmd --set controllerManager.manager.resources.requests.memory=64Mi"
    helm_cmd="$helm_cmd --wait --timeout=10m"
    
    # Execute the helm install command
    echo "Running: $helm_cmd"
    eval "$helm_cmd"
    
    if [ $? -ne 0 ]; then
        echo "Error: Failed to deploy Cedana Helm chart"
        kubectl get pods -n cedana-systems 2>/dev/null || true
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
    
    # Check if we're on bare metal or in container
    if [ -f /.dockerenv ]; then
        echo "Container environment detected, using container cleanup..."
        
        # Try graceful shutdown first
        if command -v k3s-uninstall.sh &> /dev/null; then
            echo "Running k3s uninstall script..."
            timeout 60 k3s-uninstall.sh || echo "k3s uninstall script timed out or failed"
        fi
        
        # Force kill any remaining processes
        echo "Stopping k3s processes..."
        pkill -f k3s-server || true
        pkill -f containerd || true
        pkill -f runc || true
        sleep 2
        
        # Force kill if still running
        pkill -9 -f k3s-server || true
        pkill -9 -f containerd || true
        pkill -9 -f runc || true
        
        # Clean up k3s data
        echo "Cleaning up k3s data..."
        rm -rf /var/lib/rancher/k3s || true
        rm -rf /etc/rancher/k3s || true
    else
        echo "Bare metal environment detected, using systemd cleanup..."
        
        # Stop k3s service first
        echo "Stopping k3s service..."
        sudo systemctl stop k3s || true
        sudo systemctl disable k3s || true
        
        # Run k3s uninstall script if available
        if [ -f /usr/local/bin/k3s-uninstall.sh ]; then
            echo "Running k3s uninstall script..."
            sudo /usr/local/bin/k3s-uninstall.sh || echo "k3s uninstall script failed"
        fi
        
        # Additional cleanup for bare metal
        echo "Cleaning up k3s systemd files..."
        sudo rm -f /etc/systemd/system/k3s.service || true
        sudo rm -f /etc/systemd/system/k3s.service.env || true
        sudo systemctl daemon-reload || true
        
        # Clean up k3s data directories
        echo "Cleaning up k3s data directories..."
        sudo rm -rf /var/lib/rancher/k3s || true
        sudo rm -rf /etc/rancher/k3s || true
        
        # Clean up k3s binaries
        echo "Cleaning up k3s binaries..."
        sudo rm -f /usr/local/bin/k3s || true
        sudo rm -f /usr/local/bin/k3s-uninstall.sh || true
        sudo rm -f /usr/local/bin/kubectl || true
        sudo rm -f /usr/local/bin/crictl || true
        sudo rm -f /usr/local/bin/ctr || true
        
        # Clean up containerd state
        echo "Cleaning up containerd state..."
        sudo pkill -f containerd || true
        sudo rm -rf /run/containerd || true
        sudo rm -rf /var/lib/containerd || true
    fi
    
    echo "k3s teardown complete"
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

# Function to set up k3s cluster on bare metal
setup_k3s_cluster_bare_metal() {
    echo "Setting up k3s cluster on bare metal..."
    
    # Kill any existing k3s processes first
    sudo pkill -f k3s-server || true
    sudo pkill -f containerd || true
    sleep 2
    
    # Clean up any existing k3s installation
    if [ -f /usr/local/bin/k3s-uninstall.sh ]; then
        echo "Cleaning up existing k3s installation..."
        sudo /usr/local/bin/k3s-uninstall.sh || true
        sleep 5
    fi
    
    # Install k3s with bare metal optimized settings
    echo "Installing k3s on bare metal..."
    curl -sfL https://get.k3s.io | sh -s - server \
        --write-kubeconfig-mode=644 \
        --disable=traefik \
        --disable=servicelb \
        --disable=metrics-server
    
    if [ $? -ne 0 ]; then
        echo "Failed to install k3s"
        return 1
    fi
    
    # Wait for k3s service to be ready
    echo "Waiting for k3s service to start..."
    sudo systemctl enable k3s || true
    sudo systemctl start k3s || true
    
    # Wait for kubeconfig to be available
    echo "Waiting for kubeconfig to be created..."
    local timeout=120
    local count=0
    while [ $count -lt $timeout ]; do
        if [ -f /etc/rancher/k3s/k3s.yaml ]; then
            echo "Kubeconfig created"
            sudo chmod 644 /etc/rancher/k3s/k3s.yaml
            break
        fi
        sleep 1
        count=$((count + 1))
    done
    
    if [ $count -ge $timeout ]; then
        echo "Timeout waiting for kubeconfig"
        return 1
    fi
    
    # Wait for k3s cluster to be ready
    echo "Waiting for k3s cluster to be ready..."
    timeout=180
    count=0
    while [ $count -lt $timeout ]; do
        if kubectl get nodes --kubeconfig=/etc/rancher/k3s/k3s.yaml 2>/dev/null | grep -q "Ready"; then
            echo "k3s cluster is ready"
            break
        fi
        sleep 2
        count=$((count + 2))
    done
    
    if [ $count -ge $timeout ]; then
        echo "Timeout waiting for k3s cluster to be ready"
        kubectl get nodes --kubeconfig=/etc/rancher/k3s/k3s.yaml || true
        sudo systemctl status k3s || true
        sudo journalctl -u k3s --no-pager -l || true
        return 1
    fi
    
    echo "k3s cluster is ready on bare metal"
    return 0
}

# Function to set up k3s cluster
setup_k3s_cluster() {
    # Check if we're running on bare metal or in a container
    if [ -f /.dockerenv ]; then
        echo "Container environment detected, using container-optimized setup..."
        
        # Kill any existing k3s processes first
        pkill -f k3s-server || true
        pkill -f containerd || true
        sleep 2
        
        # Start k3s with specific configuration for containers
        # Use native snapshotter to avoid overlayfs issues in Docker
        # Disable traefik to avoid port conflicts
        timeout 300 curl -sfL https://get.k3s.io | INSTALL_K3S_EXEC="server \
            --write-kubeconfig-mode=644 \
            --disable=traefik \
            --snapshotter=native \
            --container-runtime-endpoint=unix:///run/containerd/containerd.sock" sh -
        
        if [ $? -ne 0 ]; then
            echo "Failed to install k3s within timeout"
            return 1
        fi
        
        # Wait for containerd to be ready with timeout
        echo "Waiting for containerd to be ready..."
        local timeout=120
        local count=0
        while [ $count -lt $timeout ]; do
            if [ -S /run/containerd/containerd.sock ]; then
                echo "Containerd socket is ready"
                break
            fi
            sleep 1
            count=$((count + 1))
        done
        
        if [ $count -ge $timeout ]; then
            echo "Timeout waiting for containerd socket"
            return 1
        fi
        
        # Wait for k3s to be ready with timeout
        echo "Waiting for k3s to be ready..."
        timeout=120
        count=0
        while [ $count -lt $timeout ]; do
            if kubectl get nodes --kubeconfig=/etc/rancher/k3s/k3s.yaml &>/dev/null; then
                echo "k3s is ready"
                break
            fi
            sleep 1
            count=$((count + 1))
        done
        
        if [ $count -ge $timeout ]; then
            echo "Timeout waiting for k3s to be ready"
            return 1
        fi
        
        echo "k3s cluster is ready in container"
    else
        echo "Bare metal environment detected, using bare metal setup..."
        setup_k3s_cluster_bare_metal
    fi
}

# Function to patch Cedana DaemonSet for Docker environment
patch_cedana_for_docker() {
    echo "Patching Cedana DaemonSet for Docker environment..."
    
    # Wait for any DaemonSet to exist in cedana-systems namespace
    echo "Waiting for Cedana DaemonSet to be created..."
    local daemonset_name=""
    for i in $(seq 1 30); do
        # Find DaemonSet in cedana-systems namespace
        local daemonsets=$(kubectl get daemonset -n cedana-systems -o name 2>/dev/null || true)
        if [ -n "$daemonsets" ]; then
            # Get the first DaemonSet name (remove the "daemonset.apps/" prefix)
            daemonset_name=$(echo "$daemonsets" | head -1 | sed 's|daemonset.apps/||')
            echo "DaemonSet found: $daemonset_name"
            break
        fi
        echo "Waiting for DaemonSet (attempt $i/30)..."
        sleep 2
    done
    
    if [ -z "$daemonset_name" ]; then
        echo "❌ No DaemonSet found in cedana-systems namespace"
        echo "Available pods in cedana-systems:"
        kubectl get pods -n cedana-systems || true
        echo "⚠️  Continuing without DaemonSet patch - helper may not work properly"
        return 0  # Don't fail the test, just warn
    fi
    
    # Get the actual container name from the DaemonSet
    echo "Getting container information from DaemonSet..."
    local container_name=$(kubectl get daemonset "$daemonset_name" -n cedana-systems -o jsonpath='{.spec.template.spec.containers[0].name}')
    local container_image=$(kubectl get daemonset "$daemonset_name" -n cedana-systems -o jsonpath='{.spec.template.spec.containers[0].image}')
    
    echo "Container name: $container_name"
    echo "Container image: $container_image"
    
    # Create patch for privileged DaemonSet with host access
    cat > /tmp/cedana-daemonset-patch.yaml << EOF
spec:
  template:
    spec:
      hostPID: true
      hostNetwork: true
      hostIPC: true
      containers:
      - name: $container_name
        image: $container_image
        securityContext:
          privileged: true
          runAsUser: 0
          capabilities:
            add:
            - SYS_ADMIN
            - SYS_PTRACE
            - SYS_CHROOT
            - NET_ADMIN
            - DAC_OVERRIDE
            - SETUID
            - SETGID
        env:
        - name: HOST_ROOT
          value: "/host"
        - name: CONTAINER_RUNTIME
          value: "containerd"
        - name: CONTAINERD_SOCKET
          value: "/host/run/containerd/containerd.sock"
        - name: RUNC_ROOT
          value: "/host/run/containerd/runc"
        volumeMounts:
        - name: host-root
          mountPath: /host
          readOnly: true
        - name: host-proc
          mountPath: /host/proc
          readOnly: true
        - name: host-sys
          mountPath: /host/sys
          readOnly: true
        - name: containerd-sock
          mountPath: /host/run/containerd/containerd.sock
        - name: docker-sock
          mountPath: /var/run/docker.sock
      volumes:
      - name: host-root
        hostPath:
          path: /
          type: Directory
      - name: host-proc
        hostPath:
          path: /proc
          type: Directory
      - name: host-sys
        hostPath:
          path: /sys
          type: Directory
      - name: containerd-sock
        hostPath:
          path: /run/containerd/containerd.sock
          type: Socket
      - name: docker-sock
        hostPath:
          path: /var/run/docker.sock
          type: Socket
      tolerations:
      - operator: Exists
        effect: NoSchedule
      - operator: Exists
        effect: NoExecute
      - key: node-role.kubernetes.io/master
        operator: Exists
        effect: NoSchedule
      - key: node-role.kubernetes.io/control-plane
        operator: Exists
        effect: NoSchedule
EOF
    
    # Apply the patch using strategic merge
    echo "Applying DaemonSet patch..."
    kubectl patch daemonset "$daemonset_name" -n cedana-systems --type=strategic --patch-file=/tmp/cedana-daemonset-patch.yaml
    
    if [ $? -eq 0 ]; then
        echo "DaemonSet patched successfully"
        
        # Wait for DaemonSet to restart - this should succeed
        echo "Waiting for DaemonSet to restart..."
        kubectl rollout status daemonset/"$daemonset_name" -n cedana-systems --timeout=300s
        
        if [ $? -eq 0 ]; then
            echo "✅ Cedana DaemonSet configured for Docker environment"
            return 0
        else
            echo "❌ DaemonSet rollout failed"
            echo "Pod status:"
            kubectl get pods -n cedana-systems
            echo "Pod logs:"
            kubectl logs -n cedana-systems -l app=cedana-helper --tail=50 || true
            return 1
        fi
    else
        echo "❌ Failed to patch DaemonSet"
        return 1
    fi
}
