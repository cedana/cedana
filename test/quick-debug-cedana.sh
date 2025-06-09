#!/bin/bash

set -e

# Environment variables
export CEDANA_URL="https://ci.cedana.ai/v1"
export CEDANA_AUTH_TOKEN="1d0e30662b9e998abb06f4e1db9362e5fea7b21337a5a98fb5e734b7f23555fa57a43abf33f2f65847a184de9ae77cf4"

echo "=== Quick Cedana Debug Setup ==="
echo "This will quickly install k3s and Cedana to debug the helper issues"
echo ""

# Install k3s
echo "Installing k3s..."
curl -sfL https://get.k3s.io | sudo sh -

# Wait for k3s to be ready
echo "Waiting for k3s to be ready..."
sleep 10
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

# Install helm if needed
if ! command -v helm &> /dev/null; then
    echo "Installing helm..."
    curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
fi

# Install Cedana
echo "Installing Cedana..."
helm install cedana oci://registry-1.docker.io/cedana/cedana-helm \
    --create-namespace -n cedana-systems \
    --set cedanaConfig.cedanaUrl="$CEDANA_URL" \
    --set cedanaConfig.cedanaAuthToken="$CEDANA_AUTH_TOKEN" \
    --set cedanaConfig.cedanaClusterName="debug-k3s-cluster" \
    --wait --timeout=3m || echo "Helm install may have failed, continuing to debug..."

# Wait a bit for helper to start and potentially crash
echo "Waiting for helper to initialize (and potentially crash)..."
sleep 30

# Now run the debug script
echo ""
echo "=== Running Debug Script ==="
chmod +x test/debug-cedana-host-access.sh
./test/debug-cedana-host-access.sh

echo ""
echo "=== Debug Complete ===" 