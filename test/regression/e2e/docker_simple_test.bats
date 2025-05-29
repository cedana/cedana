#!/usr/bin/env bats

# bats file_tags=base,e2e,docker,setup,simple

load ../helpers/k3s
load ../helpers/propagator
load ../helpers/utils

load_lib support
load_lib assert
load_lib file

setup_file() {
    echo "Testing basic Docker environment setup..."
}

teardown_file() {
    echo "Basic Docker environment test completed"
}

@test "Docker environment: Required tools are installed and working" {
    # Test basic tools
    run which curl
    assert_success
    
    run which jq
    assert_success
    
    run which helm
    assert_success
    
    run which kubectl
    assert_success
    
    # Test helm version works
    run helm version --short
    assert_success
    assert_output --partial "v3"
    
    # Test kubectl version works (client only)
    run kubectl version --client=true
    assert_success
    assert_output --partial "Client Version"
    
    # Test jq works (separate test to avoid output mixing)
    result=$(echo '{"test": "value"}' | jq -r '.test')
    [ "$result" = "value" ]
}

@test "Docker environment: k3s binary is available" {
    # Download k3s binary for testing
    if [ ! -f /usr/local/bin/k3s ]; then
        run curl -Lo /usr/local/bin/k3s https://github.com/k3s-io/k3s/releases/latest/download/k3s
        assert_success
        
        run chmod +x /usr/local/bin/k3s
        assert_success
    fi
    
    # Test k3s binary exists and is executable
    run test -f /usr/local/bin/k3s
    assert_success
    
    run test -x /usr/local/bin/k3s
    assert_success
    
    # Test k3s help works (check for "server" command in output)
    run k3s --help
    assert_success
    assert_output --partial "server"
}

@test "Docker environment: Propagator helper functions load correctly" {
    # Test that our helper functions can be loaded
    run setup_propagator_auth "$CEDANA_AUTH_TOKEN"
    assert_success
    
    # Test basic function exists
    run type get_available_clusters
    assert_success
}

@test "Docker environment: Network connectivity works" {
    # Test external connectivity
    run curl -s --max-time 10 --connect-timeout 5 -o /dev/null -w "%{http_code}" https://www.google.com
    assert_success
    assert_output "200"
    
    # Test Cedana service reachability
    run curl -s --max-time 10 --connect-timeout 5 -o /dev/null -w "%{http_code}" https://ci.cedana.ai
    assert_success
    # Should return a valid HTTP response (not connection error)
    assert [ "$output" != "000" ]
}

@test "Docker environment: Helm can work with basic operations" {
    # Test helm can create a chart
    run helm create test-chart
    assert_success
    
    # Test chart was created
    run test -d test-chart
    assert_success
    
    # Test chart has expected structure
    run test -f test-chart/Chart.yaml
    assert_success
    
    # Clean up
    run rm -rf test-chart
    assert_success
}

@test "Docker environment: Directory structure and permissions" {
    # Test we can create needed directories
    run mkdir -p /run/containerd/runc/k8s.io
    assert_success
    
    run test -d /run/containerd/runc/k8s.io
    assert_success
    
    # Test we can set permissions
    run chmod 755 /run/containerd/runc/k8s.io
    assert_success
    
    run stat -c "%a" /run/containerd/runc/k8s.io
    assert_success
    assert_output "755"
} 