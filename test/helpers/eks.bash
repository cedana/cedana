#!/usr/bin/env bash

export CLUSTER_NAME="cedana-ci-nightly"
export AWS_REGION="us-east-1"
export TF_DIR="/src/cedana-samples/tf/eks"

install_terraform() {
    debug_log "Installing terraform..."
    
    if command -v terraform &> /dev/null; then
        debug_log "Terraform already installed"
        return 0
    fi
    
    wget -O- https://apt.releases.hashicorp.com/gpg | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
    echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list
    sudo apt-get update && sudo apt-get install -y terraform
    
    debug_log "Terraform installed successfully"
}

install_aws_cli() {
    debug_log "Installing AWS CLI..."
    
    if command -v aws &> /dev/null; then
        debug_log "AWS CLI already installed"
        return 0
    fi
    
    curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
    unzip awscliv2.zip
    sudo ./aws/install
    
    debug_log "AWS CLI installed successfully"
}

setup_eks_cluster() {
    debug_log "Setting up EKS cluster using terraform..."
    
    install_terraform
    install_aws_cli
    
    cd "$TF_DIR" || {
        debug_log "Error: Terraform directory not found: $TF_DIR"
        return 1
    }
    
    # Initialize terraform
    debug_log "Initializing terraform..."
    if ! terraform init; then
        debug_log "Error: Failed to initialize terraform"
        return 1
    fi
    
    # Apply terraform configuration
    debug_log "Applying terraform configuration..."
    if ! terraform apply -auto-approve; then
        debug_log "Error: Failed to apply terraform configuration"
        return 1
    fi
    
    # Get cluster outputs
    debug_log "Getting cluster outputs..."
    CLUSTER_ENDPOINT=$(terraform output -raw cluster_endpoint)
    CLUSTER_CA_CERT=$(terraform output -raw cluster_ca_certificate)
    CLUSTER_NAME_OUTPUT=$(terraform output -raw cluster_name)
    
    # Update kubeconfig
    debug_log "Updating kubeconfig..."
    if ! aws eks update-kubeconfig --region "$AWS_REGION" --name "$CLUSTER_NAME_OUTPUT"; then
        debug_log "Error: Failed to update kubeconfig"
        return 1
    fi
    
    # Wait for cluster to be ready
    debug_log "Waiting for EKS cluster to be ready..."
    if ! kubectl wait --for=condition=Ready node --all --timeout=300s; then
        debug_log "Error: EKS cluster nodes not ready"
        return 1
    fi
    
    debug_log "EKS cluster setup completed successfully"
}

# Function to wait for EKS node groups to be ready
wait_for_eks_node_groups() {
    debug_log "Waiting for EKS node groups to be ready..."
    
    local timeout=600
    local interval=10
    local elapsed=0
    
    while [ $elapsed -lt $timeout ]; do
        # Check if all node groups are active
        if aws eks describe-nodegroup --cluster-name "$CLUSTER_NAME" --nodegroup-name "cedana-cpu-ci-pool" --region "$AWS_REGION" --query 'nodegroup.status' --output text | grep -q "ACTIVE" && \
           aws eks describe-nodegroup --cluster-name "$CLUSTER_NAME" --nodegroup-name "cedana-1xgpu-ci-pool" --region "$AWS_REGION" --query 'nodegroup.status' --output text | grep -q "ACTIVE"; then
            debug_log "All EKS node groups are ready"
            return 0
        fi
        
        debug_log "Waiting for node groups to be ready... (elapsed: ${elapsed}s)"
        sleep "$interval"
        elapsed=$((elapsed + interval))
    done
    
    debug_log "Error: Timeout waiting for EKS node groups to be ready"
    return 1
}

# Function to install NVIDIA GPU operator
install_nvidia_gpu_operator() {
    debug_log "Installing NVIDIA GPU operator..."
    
    # Add NVIDIA Helm repository
    helm repo add nvdp https://nvidia.github.io/k8s-device-plugin
    helm repo add nvidia https://helm.ngc.nvidia.com/nvidia
    helm repo update
    
    # Install NVIDIA GPU operator
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
        --timeout=10m
    
    if [ $? -ne 0 ]; then
        debug_log "Error: Failed to install NVIDIA GPU operator"
        return 1
    fi
    
    # Wait for GPU operator pods to be ready
    debug_log "Waiting for NVIDIA GPU operator pods to be ready..."
    kubectl wait --for=condition=Ready pod -l app=nvidia-device-plugin-daemonset -n gpu-operator-resources --timeout=300s
    
    debug_log "NVIDIA GPU operator installed successfully"
}

# Function to verify GPU operator installation
verify_gpu_operator() {
    debug_log "Verifying GPU operator installation..."
    
    # Check if GPU operator pods are running
    if ! kubectl get pods -n gpu-operator-resources | grep -q "Running"; then
        debug_log "Error: GPU operator pods not running"
        return 1
    fi
    
    # Check if GPU nodes are available
    if ! kubectl get nodes -l nvidia.com/gpu.count | grep -q "Ready"; then
        debug_log "Warning: No GPU nodes found"
        return 0
    fi
    
    debug_log "GPU operator verification completed"
    return 0
}

# Function to teardown EKS cluster
teardown_eks_cluster() {
    debug_log "Tearing down EKS cluster..."
    
    cd "$TF_DIR" || {
        debug_log "Error: Terraform directory not found: $TF_DIR"
        return 1
    }
    
    # Destroy terraform resources
    debug_log "Destroying terraform resources..."
    if ! terraform destroy -auto-approve; then
        debug_log "Error: Failed to destroy terraform resources"
        return 1
    fi
    
    debug_log "EKS cluster teardown completed"
}

# Function to restart EKS cluster (not applicable for EKS, but keeping interface consistent)
restart_eks_cluster() {
    debug_log "Restarting EKS cluster..."
    
    # For EKS, we need to restart the node groups
    debug_log "Restarting EKS node groups..."
    
    # Restart CPU node group
    aws eks update-nodegroup-config --cluster-name "$CLUSTER_NAME" \
        --nodegroup-name "cedana-cpu-ci-pool" \
        --region "$AWS_REGION" \
        --scaling-config minSize=0,maxSize=3,desiredSize=0
    
    # Wait for CPU nodes to scale down
    sleep 60
    
    # Scale CPU nodes back up
    aws eks update-nodegroup-config --cluster-name "$CLUSTER_NAME" \
        --nodegroup-name "cedana-cpu-ci-pool" \
        --region "$AWS_REGION" \
        --scaling-config minSize=2,maxSize=3,desiredSize=2
    
    # Restart GPU node group
    aws eks update-nodegroup-config --cluster-name "$CLUSTER_NAME" \
        --nodegroup-name "cedana-1xgpu-ci-pool" \
        --region "$AWS_REGION" \
        --scaling-config minSize=0,maxSize=3,desiredSize=0
    
    # Wait for GPU nodes to scale down
    sleep 60
    
    # Scale GPU nodes back up
    aws eks update-nodegroup-config --cluster-name "$CLUSTER_NAME" \
        --nodegroup-name "cedana-1xgpu-ci-pool" \
        --region "$AWS_REGION" \
        --scaling-config minSize=2,maxSize=3,desiredSize=2
    
    # Wait for nodes to be ready
    wait_for_eks_node_groups
    
    debug_log "EKS cluster restart completed"
} 