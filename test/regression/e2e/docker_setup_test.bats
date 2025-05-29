#!/usr/bin/env bats

# bats file_tags=base,e2e,docker,setup,k3s,helm

load ../helpers/k3s
load ../helpers/utils

load_lib support
load_lib assert
load_lib file

setup_file() {
    echo "Testing Docker environment setup for k3s and helm..."
}

teardown_file() {
    echo "Docker environment setup test completed"
}

@test "Docker environment: Basic tools are available" {
    # Test that required tools are installed
    run which curl
    assert_success
    
    run which jq
    assert_success
    
    run which helm
    assert_success
    assert_output --partial "helm"
    
    run which kubectl
    assert_success
    assert_output --partial "kubectl"
    
    # Test helm version
    run helm version --short
    assert_success
    assert_output --partial "v3"
    
    # Test kubectl version (client) - use --client=true instead of --short
    run kubectl version --client=true
    assert_success
    assert_output --partial "Client Version"
}

@test "Docker environment: k3s installation script is available" {
    run test -f /usr/local/bin/install-k3s.sh
    assert_success
    
    run test -x /usr/local/bin/install-k3s.sh
    assert_success
}

@test "Docker environment: k3s cluster setup and basic functionality" {
    # Set up k3s cluster with containerized approach
    run setup_k3s_cluster_with_helm
    assert_success
    
    # Test that k3s is running
    run kubectl get nodes
    assert_success
    assert_output --partial "Ready"
    
    # Test that we can create a simple pod
    run kubectl run test-pod --image=nginx:alpine --restart=Never
    assert_success
    
    # Wait for pod to be created
    run kubectl wait --for=condition=Ready pod/test-pod --timeout=60s
    assert_success
    
    # Verify pod is running
    run kubectl get pod test-pod -o jsonpath='{.status.phase}'
    assert_success
    assert_output "Running"
    
    # Clean up test pod
    run kubectl delete pod test-pod
    assert_success
}

@test "Docker environment: runc root path configuration" {
    # Test that runc root path can be configured
    run configure_k3s_runc_root
    assert_success
    
    # Verify the directory exists
    run test -d /run/containerd/runc/k8s.io
    assert_success
    
    # Verify permissions
    run stat -c "%a" /run/containerd/runc/k8s.io
    assert_success
    assert_output "755"
}

@test "Docker environment: helm can connect to k3s cluster" {
    # Skip if k3s setup test failed
    if [ ! -f /etc/rancher/k3s/k3s.yaml ]; then
        skip "k3s cluster not available"
    fi
    
    # Test helm can list namespaces (basic connectivity test)
    run helm list --all-namespaces
    assert_success
    
    # Test that we can create a simple helm deployment
    run helm create test-chart
    assert_success
    
    # Deploy the test chart
    run helm install test-release ./test-chart --wait --timeout=2m
    assert_success
    
    # Verify deployment
    run kubectl get deployment test-release-test-chart
    assert_success
    
    # Clean up
    run helm uninstall test-release
    assert_success
    
    run rm -rf test-chart
    assert_success
}

@test "Docker environment: network connectivity for external services" {
    # Test that we can reach external services (needed for Cedana API)
    run curl -s --max-time 10 --connect-timeout 5 -o /dev/null -w "%{http_code}" https://www.google.com
    assert_success
    assert_output "200"
    
    # Test that we can reach the Cedana propagator service
    run curl -s --max-time 10 --connect-timeout 5 -o /dev/null -w "%{http_code}" https://ci.cedana.ai
    assert_success
    # Should return 200, 404, or similar (not connection error)
    assert [ "$output" != "000" ]
}

teardown() {
    # Clean up k3s cluster after tests
    if [ -f /etc/rancher/k3s/k3s.yaml ]; then
        teardown_k3s_cluster || true
    fi
} 