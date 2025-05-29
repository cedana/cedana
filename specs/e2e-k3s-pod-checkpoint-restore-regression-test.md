# E2E K3s Pod Checkpoint/Restore Regression Test

## Feature Description

An end-to-end regression test that sets up a complete k3s cluster environment, deploys Cedana via Helm chart, and validates pod checkpoint/restore functionality through the Cedana propagator API. This test ensures the complete integration stack works correctly from cluster setup through checkpoint/restore operations.

## Overview

This feature introduces a comprehensive regression test that validates the entire Cedana checkpoint/restore workflow in a realistic Kubernetes environment. The test leverages the existing BATS testing framework and extends the current k3s helper functions to provide full lifecycle testing of pod checkpoint and restore operations.

The test integrates with the Cedana propagator service (ci.cedana.ai/v1) to perform actual checkpoint and restore operations, ensuring that the complete stack from k3s cluster through Cedana daemon to cloud propagator service functions correctly. This provides confidence in the entire deployment and operational pipeline.

Key components include:
- Automated k3s cluster setup and teardown
- Cedana deployment via official Helm charts
- Test pod deployment (nginx)
- Checkpoint/restore API integration
- Action status polling and verification
- Resource cleanup and cluster teardown

## Current Challenge

Currently, the Cedana project lacks comprehensive end-to-end testing that validates the complete workflow from cluster setup through checkpoint/restore operations. While individual components have unit and integration tests, there's no test that verifies:

1. **Complete Integration Stack**: No test validates the full pipeline from k3s → Cedana daemon → propagator service
2. **Real-world Deployment**: Tests don't validate actual Helm chart deployments in realistic environments
3. **API Integration**: Missing validation of propagator API endpoints for checkpoint/restore operations
4. **Lifecycle Management**: No automated testing of cluster setup, operation, and cleanup workflows
5. **CI/CD Confidence**: Without end-to-end tests, deployments lack validation of the complete integration

This gap means potential issues could exist in the integration between components that wouldn't be caught by isolated testing.

## Simplified Solution

The solution provides a single BATS test file that orchestrates the complete end-to-end workflow:

```bash
# Test flow overview
setup_k3s_cluster() → deploy_cedana_helm() → deploy_test_pod() → 
checkpoint_pod() → poll_status() → restore_pod() → poll_status() → 
cleanup_resources() → teardown_cluster()
```

The test validates the core checkpoint/restore functionality by:

1. **Automated Environment Setup**: Creates a clean k3s cluster with required configuration
2. **Realistic Deployment**: Uses production Helm charts and configuration
3. **API Validation**: Tests actual propagator endpoints with real authentication
4. **Status Verification**: Polls action status to ensure operations complete successfully
5. **Complete Cleanup**: Ensures no test artifacts remain after execution

This provides confidence that the entire Cedana stack functions correctly in a realistic Kubernetes environment.

## Implementation Plan

### 1. Test Infrastructure Setup

#### 1.1 Extend k3s Helper Functions
- **File**: `test/regression/helpers/k3s.bash`
- **New Functions**:
  ```bash
  setup_k3s_cluster_with_helm() {
    # Set up k3s with Helm support
    # Configure runc root path
    # Wait for cluster ready
  }
  
  teardown_k3s_cluster() {
    # Clean shutdown of k3s
    # Remove cluster artifacts
  }
  
  deploy_cedana_helm_chart() {
    # Install Cedana via OCI registry
    # Configure with test auth token
    # Wait for pods ready
  }
  ```

#### 1.2 Propagator API Helper Functions
- **File**: `test/regression/helpers/propagator.bash`
- **Functions**:
  ```bash
  checkpoint_pod_via_api() {
    # POST /v2/checkpoint/pod
    # Return action_id
  }
  
  restore_pod_via_api() {
    # POST /v2/restore/pod  
    # Return action_id
  }
  
  poll_action_status() {
    # GET /v2/actions with polling
    # Wait for completion
  }
  
  cleanup_checkpoint() {
    # PATCH /v2/checkpoints/deprecate/{id}
  }
  ```

### 2. Main Test Implementation

#### 2.1 Test File Structure
- **Location**: `test/regression/e2e/k3s_pod_cr.bats`
- **Dependencies**: 
  - `helpers/k3s.bash`
  - `helpers/propagator.bash`
  - `helpers/utils.bash`

#### 2.2 Test Functions Implementation

```bash
#!/usr/bin/env bats

load ../helpers/k3s
load ../helpers/propagator  
load ../helpers/utils

setup_file() {
    # Global test setup
    setup_k3s_cluster_with_helm
    deploy_cedana_helm_chart
}

teardown_file() {
    # Global test cleanup
    teardown_k3s_cluster
}

@test "E2E: Pod checkpoint and restore via propagator API" {
    # Deploy nginx test pod
    kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test-nginx
  namespace: default
spec:
  containers:
  - name: nginx
    image: nginx:latest
    ports:
    - containerPort: 80
EOF

    # Wait for pod ready
    kubectl wait --for=condition=Ready pod/test-nginx --timeout=60s
    
    # Checkpoint pod
    local action_id=$(checkpoint_pod_via_api "test-nginx" "/run/containerd/runc/k8s.io" "default" "$CLUSTER_ID")
    assert [ -n "$action_id" ]
    
    # Poll checkpoint status
    poll_action_status "$action_id" "checkpoint"
    
    # Restore pod  
    local restore_action_id=$(restore_pod_via_api "$action_id" "$CLUSTER_ID")
    assert [ -n "$restore_action_id" ]
    
    # Poll restore status
    poll_action_status "$restore_action_id" "restore"
    
    # Verify pod is running
    kubectl get pod test-nginx
    assert_success
    
    # Cleanup
    cleanup_checkpoint "$action_id"
    kubectl delete pod test-nginx
}
```

### 3. Configuration and Environment

#### 3.1 Environment Variables
- `CEDANA_AUTH_TOKEN`: Pre-configured test token
- `CEDANA_URL`: Set to `ci.cedana.ai/v1`
- `CLUSTER_ID`: Placeholder (to be implemented when registration available)

#### 3.2 Helm Chart Configuration
```yaml
# Override values for testing
cedanaConfig:
  cedanaAuthToken: "${CEDANA_AUTH_TOKEN}"
  cedanaUrl: "ci.cedana.ai/v1"

# Minimal resource configuration for CI
controllerManager:
  manager:
    resources:
      limits:
        cpu: 200m
        memory: 128Mi
      requests:
        cpu: 100m
        memory: 64Mi
```

### 4. CI Integration

#### 4.1 Test Execution Requirements
- **Runtime**: Standard CI environment (no GPU/privileged containers)
- **Dependencies**: kubectl, helm, k3s
- **Timeout**: 10 minutes maximum
- **Parallel Execution**: Safe (uses unique cluster per test)

#### 4.2 Test Tags and Categorization
```bash
# bats file_tags=e2e,k3s,propagator,checkpoint,restore
```

## Usage Example (Client Perspective)

### Running the Test Locally

```bash
# Set required environment variables
export CEDANA_AUTH_TOKEN="fa4318d1569bc89ac95c1223bbb41719e737640027c87200714204cb813de8a74546a5ec647052bcf19c507ca7013685"

# Run the specific e2e test
cd /path/to/cedana
bats test/regression/e2e/k3s_pod_cr.bats

# Or run with verbose output
bats -t test/regression/e2e/k3s_pod_cr.bats
```

### Expected Test Output

```
✓ E2E: Pod checkpoint and restore via propagator API
  ├── Setting up k3s cluster...
  ├── Deploying Cedana via Helm...
  ├── Deploying test nginx pod...
  ├── Checkpointing pod via API...
  ├── Polling checkpoint status... [COMPLETED]
  ├── Restoring pod via API...
  ├── Polling restore status... [COMPLETED] 
  ├── Verifying pod state...
  ├── Cleaning up checkpoint...
  └── Test completed successfully

1 test, 0 failures
```

### Integration in CI Pipeline

```yaml
# Example GitHub Actions step
- name: Run E2E K3s Tests
  env:
    CEDANA_AUTH_TOKEN: ${{ secrets.CEDANA_TEST_TOKEN }}
  run: |
    bats test/regression/e2e/k3s_pod_cr.bats
```

## Limitations

### 1. Cluster Registration Dependency
- **Current**: Test assumes `CLUSTER_ID` is available
- **Limitation**: Cluster registration API not yet implemented
- **Workaround**: Hard-coded placeholder until registration endpoint available

### 2. Single Pod Testing
- **Scope**: Only tests basic nginx pod checkpoint/restore
- **Missing**: Multi-container pods, stateful workloads, persistent volumes
- **Rationale**: Focused on core checkpoint/restore validation

### 3. Network State Validation
- **Current**: Tests pod existence post-restore
- **Missing**: Validation of network connectivity, service endpoints
- **Impact**: May miss network-related restore issues

### 4. Resource Constraints  
- **Environment**: CI environments with limited resources
- **Impact**: Cannot test resource-intensive workloads or GPU scenarios
- **Mitigation**: Focused on lightweight test scenarios

### 5. Propagator Service Dependency
- **External**: Relies on ci.cedana.ai service availability
- **Risk**: Test failures if service unavailable
- **Mitigation**: Appropriate timeout and error handling

## Future Considerations

### 1. Enhanced Test Coverage
- **Stateful Workloads**: Add tests for StatefulSets, PersistentVolumes
- **Multi-Container Pods**: Test complex pod configurations
- **Service Mesh**: Validate checkpoint/restore with Istio/Linkerd
- **Custom Resources**: Test CRDs and operators

### 2. Performance Testing
- **Benchmarks**: Add timing measurements for checkpoint/restore operations
- **Scale Testing**: Test multiple concurrent checkpoint/restore operations
- **Resource Usage**: Monitor memory/CPU usage during operations

### 3. Error Scenario Testing
- **Failure Modes**: Test checkpoint/restore failures and recovery
- **Network Issues**: Simulate network partitions during operations
- **Resource Exhaustion**: Test behavior under resource constraints

### 4. Advanced Configurations
- **Security Contexts**: Test with various security policies
- **Resource Limits**: Test pods with CPU/memory limits
- **Affinity Rules**: Test with node/pod affinity configurations

### 5. Integration Enhancements
- **Multiple Clusters**: Test cross-cluster checkpoint/restore
- **Different K8s Versions**: Test compatibility across Kubernetes versions
- **Alternative Runtimes**: Test with different container runtimes

### 6. Monitoring and Observability
- **Metrics Collection**: Integrate with Prometheus/Grafana for test metrics
- **Trace Analysis**: Add distributed tracing for operation analysis
- **Log Aggregation**: Centralized logging for test execution analysis

## Conclusion

The E2E K3s Pod Checkpoint/Restore Regression Test provides essential validation of the complete Cedana integration stack. This test ensures that the core checkpoint and restore functionality works correctly in a realistic Kubernetes environment, providing confidence for production deployments.

The implementation leverages existing BATS testing infrastructure while extending k3s helper functions to support comprehensive end-to-end testing. The test validates the complete workflow from cluster setup through checkpoint/restore operations to cleanup, ensuring no integration issues exist between components.

While the current implementation focuses on core functionality validation with basic workloads, the framework provides a foundation for expanding test coverage to include more complex scenarios, performance benchmarks, and error condition testing. This approach ensures that the Cedana checkpoint/restore capabilities remain reliable and robust as the project evolves.

The test will integrate seamlessly into existing CI pipelines, providing automated validation of the complete integration stack with each code change, significantly improving deployment confidence and reducing the risk of integration issues in production environments. 