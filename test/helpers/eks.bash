#!/usr/bin/env bash

export CLUSTER_NAME="cedana-ci-nightly"
export AWS_REGION="us-east-1"

# Install AWS CLI if not available
install_aws_cli() {
    debug_log "Installing AWS CLI..."

    if command -v aws &>/dev/null; then
        debug_log "AWS CLI already installed"
        return 0
    fi

    # Try to install AWS CLI v2
    curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "/tmp/awscliv2.zip"
    if [ $? -eq 0 ]; then
        cd /tmp
        unzip -q awscliv2.zip
        sudo ./aws/install
        rm -rf aws awscliv2.zip
        debug_log "AWS CLI installed successfully"
        return 0
    else
        debug_log "Warning: Failed to install AWS CLI, will continue without it"
        return 1
    fi
}

# Install Helm if not available
install_helm() {
    debug_log "Installing Helm..."

    if command -v helm &>/dev/null; then
        debug_log "Helm already installed"
        return 0
    fi

    # Try to install Helm
    curl -fsSL -o /tmp/get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
    if [ $? -eq 0 ]; then
        chmod 700 /tmp/get_helm.sh
        ./tmp/get_helm.sh
        # Refresh PATH to ensure helm is found
        export PATH="$PATH:/usr/local/bin"
        if command -v helm &>/dev/null; then
            debug_log "Helm installed successfully"
            return 0
        else
            debug_log "Error: Helm installation completed but helm command not found in PATH"
            debug_log "Current PATH: $PATH"
            return 1
        fi
    fi

    debug_log "Warning: Failed to install Helm, will continue without it"
    return 1
}

# Install kubectl if not available
install_kubectl() {
    debug_log "Installing kubectl..."

    if command -v kubectl &>/dev/null; then
        debug_log "kubectl already installed"
        return 0
    fi

    # Try to install kubectl
    local kubectl_version=$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)
    curl -LO "https://storage.googleapis.com/kubernetes-release/release/${kubectl_version}/bin/linux/amd64/kubectl"
    if [ $? -eq 0 ]; then
        chmod +x kubectl
        sudo mv kubectl /usr/local/bin/
        # Refresh PATH to ensure kubectl is found
        export PATH="$PATH:/usr/local/bin"
        if command -v kubectl &>/dev/null; then
            debug_log "kubectl installed successfully"
            return 0
        else
            debug_log "Error: kubectl installation completed but kubectl command not found in PATH"
            debug_log "Current PATH: $PATH"
            return 1
        fi
    fi

    debug_log "Warning: Failed to install kubectl, will continue without it"
    return 1
}

# Configure AWS credentials from GitHub secrets
configure_aws_credentials() {
    debug_log "Configuring AWS credentials..."

    if [ -n "$AWS_ACCESS_KEY_ID" ] && [ -n "$AWS_SECRET_ACCESS_KEY" ]; then
        debug_log "Using AWS credentials from environment variables"
        export AWS_ACCESS_KEY_ID
        export AWS_SECRET_ACCESS_KEY

        # Configure AWS CLI if available
        if command -v aws &>/dev/null; then
            aws configure set aws_access_key_id "$AWS_ACCESS_KEY_ID"
            aws configure set aws_secret_access_key "$AWS_SECRET_ACCESS_KEY"
            aws configure set default.region "$AWS_REGION"
        fi

        debug_log "AWS credentials configured successfully"
        return 0
    else
        debug_log "Warning: AWS credentials not found in environment variables"
        debug_log "Make sure AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are set"
        return 1
    fi
}

setup_eks_cluster() {
    debug_log "Setting up connection to existing EKS cluster..."

    # Install required tools
    install_aws_cli
    install_helm
    install_kubectl

    configure_aws_credentials

    # Check if AWS CLI is available
    if ! command -v aws &>/dev/null; then
        debug_log "Warning: AWS CLI not available, skipping cluster verification"
        return 0
    fi

    debug_log "Updating kubeconfig..."
    if ! aws eks update-kubeconfig --region "$AWS_REGION" --name "cedana-ci-nightly"; then
        debug_log "Error: Failed to update kubeconfig"
        return 1
    fi

    debug_log "Waiting for EKS cluster to be ready..."
    if ! kubectl wait --for=condition=Ready node --all --timeout=300s; then
        debug_log "Error: EKS cluster nodes not ready"
        return 1
    fi

    debug_log "EKS cluster connection established successfully"
}

wait_for_eks_node_groups() {
    debug_log "Checking EKS node groups..."

    # Skip if AWS CLI not available
    if ! command -v aws &>/dev/null; then
        debug_log "Warning: AWS CLI not available, skipping node group verification"
        return 0
    fi

    local timeout=300
    local interval=10
    local elapsed=0

    while [ $elapsed -lt $timeout ]; do
        if aws eks describe-nodegroup --cluster-name "cedana-ci-nightly" --nodegroup-name "cedana-cpu-ci-pool" --region "$AWS_REGION" --query 'nodegroup.status' --output text 2>/dev/null | grep -q "ACTIVE" &&
            aws eks describe-nodegroup --cluster-name "cedana-ci-nightly" --nodegroup-name "cedana-1xgpu-ci-pool" --region "$AWS_REGION" --query 'nodegroup.status' --output text 2>/dev/null | grep -q "ACTIVE"; then
            debug_log "All EKS node groups are ready"
            return 0
        fi

        debug_log "Waiting for node groups to be ready... (elapsed: ${elapsed}s)"
        sleep "$interval"
        elapsed=$((elapsed + interval))
    done

    debug_log "Warning: Timeout waiting for EKS node groups to be ready"
    return 0 # Don't fail the test, just warn
}

install_nvidia_gpu_operator() {
    debug_log "Installing NVIDIA GPU operator..."

    # Ensure helm is available
    if ! command -v helm &>/dev/null; then
        install_helm
        if [ $? -ne 0 ]; then
            debug_log "Error: Helm is required for GPU operator installation"
            return 1
        fi
    fi

    # Ensure kubectl is available
    if ! command -v kubectl &>/dev/null; then
        install_kubectl
        if [ $? -ne 0 ]; then
            debug_log "Error: kubectl is required for GPU operator installation"
            return 1
        fi
    fi

    # Check if GPU operator is already installed and working
    if kubectl get pods -n gpu-operator-resources &>/dev/null; then
        debug_log "GPU operator namespace exists, checking if it's working..."
        if kubectl get pods -n gpu-operator-resources -l app=nvidia-device-plugin-daemonset --no-headers | grep -q "Running"; then
            debug_log "NVIDIA GPU operator is already installed and running, skipping installation"
            return 0
        else
            debug_log "GPU operator namespace exists but pods are not running, will reinstall"
        fi
    fi

    helm repo add nvdp https://nvidia.github.io/k8s-device-plugin
    helm repo add nvidia https://helm.ngc.nvidia.com/nvidia
    helm repo update

    helm install --generate-name nvidia/gpu-operator \
        --set driver.enabled=true \
        --set toolkit.enabled=true \
        --set devicePlugin.enabled=true \
        --set migManager.enabled=false \
        --set gfd.enabled=false \
        --set validator.enabled=true \
        --set operator.defaultRuntime=containerd \
        --namespace gpu-operator-resources \
        --create-namespace \
        --wait \
        --timeout=20m

    if [ $? -ne 0 ]; then
        debug_log "Error: Failed to install NVIDIA GPU operator"
        return 1
    fi

    debug_log "Waiting for NVIDIA GPU operator pods to be ready..."

    # Show initial pod status
    debug_log "Initial GPU operator pod status:"
    kubectl get pods -n gpu-operator-resources -o wide

    kubectl wait --for=condition=Ready pod -l app=nvidia-device-plugin-daemonset -n gpu-operator-resources --timeout=600s

    # Show final pod status
    debug_log "Final GPU operator pod status:"
    kubectl get pods -n gpu-operator-resources -o wide

    debug_log "NVIDIA GPU operator installed successfully"
}

verify_gpu_operator() {
    debug_log "Verifying GPU operator installation..."

    # Ensure kubectl is available
    if ! command -v kubectl &>/dev/null; then
        install_kubectl
        if [ $? -ne 0 ]; then
            debug_log "Error: kubectl is required for GPU operator verification"
            return 1
        fi
    fi

    if ! kubectl get pods -n gpu-operator-resources | grep -q "Running"; then
        debug_log "Error: GPU operator pods not running"
        return 1
    fi

    if ! kubectl get nodes -l nvidia.com/gpu.count | grep -q "Ready"; then
        debug_log "Warning: No GPU nodes found"
        return 0
    fi

    debug_log "GPU operator verification completed"
    return 0
}

teardown_eks_cluster() {
    debug_log "Tearing down EKS cluster connection..."

    # No actual teardown needed for existing cluster
    debug_log "EKS cluster connection teardown completed"
}
