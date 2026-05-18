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

setup_cluster() {
    _install_aws_cli
    _configure_aws_credentials
    debug_log "Setting up EKS Karpenter cluster $EKS_KARPENTER_CLUSTER..."

    if aws eks update-kubeconfig \
        --region "$AWS_REGION" \
        --name "$EKS_KARPENTER_CLUSTER" \
        --kubeconfig "$KUBECONFIG" &>/dev/null; then
        debug_log "Fetched kubeconfig for $EKS_KARPENTER_CLUSTER"
    else
        debug_log "Failed to fetch kubeconfig for $EKS_KARPENTER_CLUSTER"
        return 1
    fi

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
    local nodepool_name="$KARPENTER_NODEPOOL"

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
        karpenter.sh/discovery: "${EKS_KARPENTER_SUBNET_SELECTOR}"
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
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
        - key: kubernetes.io/os
          operator: In
          values: ["linux"]
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["spot"]
        - key: karpenter.k8s.aws/instance-category
          operator: In
          values: ["c", "t", "m"]
        - key: karpenter.k8s.aws/instance-family
          operator: In
          values: ["t3a","c5a","m6a","m5a","c6a","c7a","c5","c6","c7"]
        - key: karpenter.k8s.aws/instance-generation
          operator: Gt
          values: ["2"]
      nodeClassRef:
        group: karpenter.k8s.aws
        kind: EC2NodeClass
        name: $nodepool_name
      expireAfter: 720h # 30 * 24h = 720h
  limits:
    cpu: "100"
    memory: "100Gi"

  disruption:
    consolidationPolicy: WhenEmpty
    consolidateAfter: 5m
EOF

    # Wait for NodePool to be ready
    kubectl wait --for=condition=Ready nodepool/"$nodepool_name" --timeout=60s 2>/dev/null || {
        debug_log "NodePool may not have Ready condition, continuing..."
    }

    debug_log "Spot NodePool $nodepool_name created"
}
