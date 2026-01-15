#!/usr/bin/env bash

################################
### EKS + Karpenter Provider ###
################################
#
# Provider for EKS clusters with Karpenter installed.
# Used for spot instance interruption testing.
#
# Environment variables:
#   AWS_ACCESS_KEY_ID         - AWS access key
#   AWS_SECRET_ACCESS_KEY     - AWS secret key
#   AWS_REGION                - AWS region (default: us-east-1)
#   EKS_KARPENTER_CLUSTER     - EKS cluster name with Karpenter installed
#   KARPENTER_NODEPOOL        - NodePool name for spot instances (default: cedana-spot-test)
#   KARPENTER_NAMESPACE       - Namespace where Karpenter is installed (default: kube-system)
#

export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
export EKS_KARPENTER_CLUSTER="${EKS_KARPENTER_CLUSTER:-cedana-karpenter-ci}"
export AWS_REGION="${AWS_REGION:-us-east-1}"
export KARPENTER_NODEPOOL="${KARPENTER_NODEPOOL:-cedana-spot-test}"
export KARPENTER_NAMESPACE="${KARPENTER_NAMESPACE:-kube-system}"

_install_aws_cli() {
    debug_log "Installing AWS CLI..."

    if command -v aws &>/dev/null; then
        debug_log "AWS CLI already installed"
        return 0
    fi

    local arch
    arch=$(uname -m)

    debug curl "https://awscli.amazonaws.com/awscli-exe-linux-${arch}.zip" -o "/tmp/awscli.zip"
    debug unzip /tmp/awscli.zip -d /tmp
    /tmp/aws/install --update
    rm -rf /tmp/awscli.zip /tmp/aws
    debug_log "AWS CLI installed"
}

_configure_aws_credentials() {
    debug_log "Configuring AWS credentials..."

    if [ -z "$AWS_ACCESS_KEY_ID" ] || [ -z "$AWS_SECRET_ACCESS_KEY" ] || [ -z "$AWS_REGION" ]; then
        error_log "AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY or AWS_REGION are not set"
        return 1
    fi

    aws configure set aws_access_key_id "$AWS_ACCESS_KEY_ID"
    aws configure set aws_secret_access_key "$AWS_SECRET_ACCESS_KEY"
    aws configure set default.region "$AWS_REGION"

    debug_log "AWS credentials configured"
}

_verify_karpenter() {
    debug_log "Verifying Karpenter installation..."

    # Check common namespaces for Karpenter
    local namespaces=("$KARPENTER_NAMESPACE" "karpenter" "kube-system")

    for ns in "${namespaces[@]}"; do
        if kubectl get deployment karpenter -n "$ns" &>/dev/null; then
            debug_log "Karpenter found in namespace $ns"
            KARPENTER_NAMESPACE="$ns"
            export KARPENTER_NAMESPACE
            return 0
        fi
    done

    error_log "Karpenter deployment not found in cluster"
    return 1
}

setup_cluster() {
    _install_aws_cli
    _configure_aws_credentials
    debug_log "Setting up EKS Karpenter cluster $EKS_KARPENTER_CLUSTER..."

    aws eks update-kubeconfig \
        --region "$AWS_REGION" \
        --name "$EKS_KARPENTER_CLUSTER" \
        --kubeconfig "$KUBECONFIG"

    # Verify Karpenter is running
    _verify_karpenter || return 1

    # Create spot NodePool for testing
    create_spot_nodepool

    debug_log "EKS Karpenter cluster $EKS_KARPENTER_CLUSTER is ready"
}

teardown_cluster() {
    debug_log "Tearing down EKS Karpenter cluster resources..."

    # Clean up test NodePool
    kubectl delete nodepool "$KARPENTER_NODEPOOL" --ignore-not-found=true 2>/dev/null || true

    # Clean up EC2NodeClass
    kubectl delete ec2nodeclass cedana-spot-test --ignore-not-found=true 2>/dev/null || true

    debug_log "EKS Karpenter teardown complete"
}

# Create a spot-only NodePool for testing
create_spot_nodepool() {
    local nodepool_name="${1:-$KARPENTER_NODEPOOL}"

    debug_log "Creating spot NodePool $nodepool_name..."

    # First create the EC2NodeClass
    kubectl apply -f - <<EOF
apiVersion: karpenter.k8s.aws/v1
kind: EC2NodeClass
metadata:
  name: cedana-spot-test
spec:
  amiSelectorTerms:
    - alias: al2023@latest
  role: "KarpenterNodeRole-${EKS_KARPENTER_CLUSTER}"
  subnetSelectorTerms:
    - tags:
        karpenter.sh/discovery: "${EKS_KARPENTER_CLUSTER}"
  securityGroupSelectorTerms:
    - tags:
        karpenter.sh/discovery: "${EKS_KARPENTER_CLUSTER}"
  metadataOptions:
    httpEndpoint: enabled
    httpProtocolIPv6: disabled
    httpPutResponseHopLimit: 1
    httpTokens: required
EOF

    # Create NodePool that only uses spot instances
    kubectl apply -f - <<EOF
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: $nodepool_name
spec:
  template:
    spec:
      requirements:
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["spot"]
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["m5.large", "m5.xlarge", "m5a.large", "m5a.xlarge", "m6i.large", "m6i.xlarge"]
      nodeClassRef:
        group: karpenter.k8s.aws
        kind: EC2NodeClass
        name: cedana-spot-test
      taints:
        - key: "cedana.ai/spot-test"
          value: "true"
          effect: NoSchedule
  limits:
    cpu: 100
    memory: 100Gi
  disruption:
    consolidationPolicy: WhenEmpty
    consolidateAfter: 30s
EOF

    # Wait for NodePool to be ready
    kubectl wait --for=condition=Ready nodepool/"$nodepool_name" --timeout=60s 2>/dev/null || {
        debug_log "NodePool may not have Ready condition, continuing..."
    }

    debug_log "Spot NodePool $nodepool_name created"
}

# Simulate spot interruption by terminating the EC2 instance
simulate_spot_interruption() {
    local node_name="$1"

    if [ -z "$node_name" ]; then
        error_log "simulate_spot_interruption requires node_name"
        return 1
    fi

    debug_log "Simulating spot interruption for node $node_name..."

    # Get the EC2 instance ID from the node's provider ID
    local provider_id
    provider_id=$(kubectl get node "$node_name" -o jsonpath='{.spec.providerID}')

    local instance_id
    instance_id=$(echo "$provider_id" | sed 's|.*/||')

    if [ -z "$instance_id" ]; then
        error_log "Failed to get instance ID for node $node_name"
        return 1
    fi

    debug_log "Terminating EC2 instance $instance_id..."

    aws ec2 terminate-instances --instance-ids "$instance_id" --region "$AWS_REGION"

    debug_log "Spot interruption simulated for instance $instance_id (node $node_name)"
}

# Get the node where a pod is running
get_pod_node() {
    local pod_name="$1"
    local namespace="${2:-$NAMESPACE}"

    kubectl get pod "$pod_name" -n "$namespace" -o jsonpath='{.spec.nodeName}' 2>/dev/null
}

# Wait for a pod to be scheduled on a Karpenter-provisioned spot node
wait_for_spot_node() {
    local pod_name="$1"
    local namespace="${2:-$NAMESPACE}"
    local timeout="${3:-300}"

    debug_log "Waiting for pod $pod_name to be scheduled on spot node (timeout: ${timeout}s)..."

    local elapsed=0
    local poll_interval=5

    while [ $elapsed -lt $timeout ]; do
        local node_name
        node_name=$(get_pod_node "$pod_name" "$namespace")

        if [ -n "$node_name" ]; then
            # Verify it's a spot instance via Karpenter label
            local capacity_type
            capacity_type=$(kubectl get node "$node_name" -o jsonpath='{.metadata.labels.karpenter\.sh/capacity-type}' 2>/dev/null)

            if [ "$capacity_type" = "spot" ]; then
                debug_log "Pod $pod_name scheduled on spot node $node_name"
                echo "$node_name"
                return 0
            elif [ -n "$capacity_type" ]; then
                debug_log "Pod scheduled on $capacity_type node, waiting for spot..."
            fi
        fi

        sleep $poll_interval
        ((elapsed += poll_interval))
    done

    error_log "Timeout waiting for pod $pod_name to be scheduled on spot node"
    return 1
}

# Create a pod spec with spot tolerations and affinity
spot_pod_spec() {
    local image="$1"
    local args="$2"
    local namespace="${3:-$NAMESPACE}"

    local name
    name=test-spot-$(unix_nano)

    local spec=/tmp/pod-${name}.yaml
    cat >"$spec" <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: "$namespace"
  labels:
    app: "$name"
    cedana.ai/spot-test: "true"
spec:
  tolerations:
    - key: "cedana.ai/spot-test"
      operator: "Equal"
      value: "true"
      effect: NoSchedule
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: karpenter.sh/capacity-type
                operator: In
                values: ["spot"]
  containers:
  - name: "$name"
    image: $image
    command: ["/bin/sh", "-c"]
    resources:
      requests:
        cpu: "500m"
        memory: "512Mi"
EOF

    if [[ -n "$args" ]]; then
        printf "    args:\n" >>"$spec"
        printf "      - |\n" >>"$spec"
        while IFS= read -r line; do
            printf "        %s\n" "$line" >>"$spec"
        done <<<"$args"
    fi

    echo "$spec"
}

# Create a GPU pod spec with spot tolerations
spot_pod_spec_gpu() {
    local image="$1"
    local args="$2"
    local gpus="${3:-1}"
    local namespace="${4:-$NAMESPACE}"

    local name
    name=test-spot-cuda-$(unix_nano)

    local spec=/tmp/pod-${name}.yaml
    cat >"$spec" <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: "$namespace"
  labels:
    app: "$name"
    cedana.ai/spot-test: "true"
spec:
  runtimeClassName: cedana
  tolerations:
    - key: "cedana.ai/spot-test"
      operator: "Equal"
      value: "true"
      effect: NoSchedule
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: karpenter.sh/capacity-type
                operator: In
                values: ["spot"]
  containers:
  - name: "$name"
    image: $image
    command: ["/bin/sh", "-c"]
    resources:
      limits:
        nvidia.com/gpu: "$gpus"
EOF

    if [[ -n "$args" ]]; then
        printf "    args:\n" >>"$spec"
        printf "      - |\n" >>"$spec"
        while IFS= read -r line; do
            printf "        %s\n" "$line" >>"$spec"
        done <<<"$args"
    fi

    echo "$spec"
}
