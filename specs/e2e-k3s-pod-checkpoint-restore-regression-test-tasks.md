# E2E K3s Pod Checkpoint/Restore Regression Test Implementation

Implementation tasks for creating an end-to-end regression test that sets up a complete k3s cluster environment, deploys Cedana via Helm chart, and validates pod checkpoint/restore functionality through the Cedana propagator API. The test runs inside a Docker container using the existing test infrastructure.

## Completed Tasks

- [x] **Create e2e test directory structure**
  - Create `test/regression/e2e/` directory
  - Set up proper permissions and organization

- [x] **Extend k3s helper functions for containerized environment**
  - Add `setup_k3s_cluster_with_helm()` function to `test/regression/helpers/k3s.bash`
  - Add `teardown_k3s_cluster()` function to `test/regression/helpers/k3s.bash`
  - Add `deploy_cedana_helm_chart()` function to `test/regression/helpers/k3s.bash`
  - Add `wait_for_k3s_ready()` utility function
  - Add `configure_k3s_runc_root()` function for container environment
  - Ensure k3s works properly inside Docker container

## In Progress Tasks

_No tasks in progress yet._

## Future Tasks

### 1. Test Infrastructure Setup

- [ ] **Create propagator API helper functions**
  - Create `test/regression/helpers/propagator.bash` file
  - Implement `checkpoint_pod_via_api()` function for POST /v2/checkpoint/pod
  - Implement `restore_pod_via_api()` function for POST /v2/restore/pod
  - Implement `poll_action_status()` function for GET /v2/actions polling
  - Implement `cleanup_checkpoint()` function for PATCH /v2/checkpoints/deprecate/{id}
  - Implement `get_available_clusters()` function for GET /v1/cluster endpoint
  - Add authentication helper for bearer token setup
  - Add JSON response parsing utilities
  - Add error handling and timeout mechanisms

### 2. Docker Environment Setup

- [ ] **Update test Dockerfile**
  - Ensure k3s is properly installed in `test/Dockerfile`
  - Add helm installation to container
  - Verify kubectl is available
  - Add any missing dependencies for k3s cluster setup
  - Configure container networking for k3s

- [ ] **Create Docker test runner script**
  - Create script to build test Docker image
  - Add script to run container with proper environment variables
  - Mount source code into container
  - Configure container networking and privileges if needed
  - Add cleanup of Docker containers after test

- [ ] **Configure containerized test environment**
  - Set up environment variable passing to Docker container
  - Configure volume mounts for test artifacts
  - Ensure proper container cleanup after tests
  - Add support for parallel test execution in containers

### 3. Main Test Implementation

- [ ] **Create main test file**
  - Create `test/regression/e2e/k3s_pod_cr.bats` file
  - Add BATS shebang and proper file tags: `# bats file_tags=e2e,k3s,propagator,checkpoint,restore`
  - Load required helper files (k3s, propagator, utils)

- [ ] **Implement test setup/teardown for containerized environment**
  - Implement `setup_file()` function for global test setup inside container
  - Implement `teardown_file()` function for global test cleanup inside container
  - Add environment variable validation
  - Add dependency checks (kubectl, helm, k3s) within container
  - Handle container-specific k3s setup requirements

- [ ] **Implement core test logic**
  - Create nginx test pod deployment logic
  - Add pod readiness verification with kubectl wait
  - Implement checkpoint operation via propagator API
  - Add checkpoint status polling and verification
  - Implement restore operation via propagator API
  - Add restore status polling and verification
  - Add post-restore pod state verification
  - Implement cleanup logic for test artifacts

### 4. Configuration and Environment

- [ ] **Configure test environment variables for Docker**
  - Set up CEDANA_AUTH_TOKEN handling in container
  - Set up CEDANA_URL configuration (ci.cedana.ai/v1)
  - Add CLUSTER_ID retrieval from /v1/cluster endpoint (or fallback to placeholder)
  - Add RUNC_ROOT configuration (/run/containerd/runc/k8s.io)
  - Configure environment variable passing from CI to Docker container

- [ ] **Create Helm chart configuration for containerized environment**
  - Create test-specific values.yaml overrides
  - Configure minimal resource limits for container environment
  - Set up authentication token injection
  - Configure cedana-helm chart parameters for container networking

- [ ] **Add cluster ID management**
  - Add function to retrieve existing cluster IDs via GET /v1/cluster endpoint
  - Implement cluster ID selection logic (use existing cluster or create placeholder)
  - Add `get_available_clusters()` function to propagator.bash helpers
  - Document cluster registration requirement for new clusters
  - Add TODO markers for future cluster registration API integration
  - Handle case where no clusters exist for the organization

### 5. Testing and Validation

- [ ] **Create test validation scripts**
  - Add pod deployment verification
  - Add API response validation
  - Add action status verification logic
  - Add timeout and retry mechanisms
  - Ensure validations work within container environment

- [ ] **Add error handling for containerized tests**
  - Implement test failure cleanup within container
  - Add proper error messages and logging
  - Add debug output for troubleshooting container issues
  - Handle propagator service unavailability
  - Add container-specific error scenarios

- [ ] **Add test documentation**
  - Create usage documentation in test file comments
  - Add troubleshooting guide for Docker container issues
  - Document environment requirements for containerized testing
  - Add example test run outputs

### 6. CI Integration with Docker

- [ ] **Add Docker-based CI configuration**
  - Update CI pipeline to build test Docker image
  - Configure CI to run tests inside Docker container
  - Add environment variable setup for Docker container
  - Configure test timeouts (10 minutes maximum)
  - Add test artifact extraction from container

- [ ] **Add dependency management for Docker environment**
  - Ensure all dependencies are in Docker image
  - Verify k3s installation and setup in container
  - Ensure helm and kubectl work properly in container
  - Add networking configuration for container communication

- [ ] **Create Docker test isolation**
  - Ensure Docker containers can run in parallel safely
  - Add unique container naming per test run
  - Add proper Docker container cleanup
  - Add test result reporting from container

### 7. Documentation and Examples

- [ ] **Create Docker-based usage documentation**
  - Add README section for Docker-based e2e testing
  - Document required environment variables for Docker
  - Add local development setup instructions with Docker
  - Create troubleshooting guide for containerized testing

- [ ] **Add Docker test examples**
  - Document expected test output from container
  - Add example CI pipeline configuration with Docker
  - Create local testing scripts using Docker
  - Add debugging examples for container issues

## Implementation Plan

### Phase 1: Infrastructure (Tasks 1-2)
Set up the foundational test infrastructure including helper functions and Docker environment setup. This provides the containerized framework for all subsequent testing functionality.

### Phase 2: Core Functionality (Tasks 3-4)
Implement the core test logic including environment configuration, API integration, and validation mechanisms within the Docker container. This creates the working end-to-end test.

### Phase 3: Integration & Polish (Tasks 5-7)
Add Docker-based CI integration, documentation, and examples to make the test ready for production use and easy for developers to run in containerized environments.

### Relevant Files

- `test/regression/e2e/k3s_pod_cr.bats` - Main E2E test file (🔄 To be created)
- `test/regression/helpers/k3s.bash` - Extended k3s helper functions (🔄 To be modified)
- `test/regression/helpers/propagator.bash` - Propagator API helper functions (🔄 To be created)
- `test/Dockerfile` - Test container environment (🔄 To be modified)
- `test/entrypoint.sh` - Container entrypoint script (🔄 May need modification)
- `specs/e2e-k3s-pod-checkpoint-restore-regression-test.md` - Feature specification (✅ Created)

### Dependencies (Inside Docker Container)

- kubectl CLI tool
- helm CLI tool  
- k3s Kubernetes distribution
- BATS testing framework
- jq for JSON processing
- curl for API calls
- containerd (already in test/Dockerfile)
- Access to ci.cedana.ai propagator service
- Valid CEDANA_AUTH_TOKEN

### Technical Notes

- Test runs inside Docker container using existing `test/Dockerfile` infrastructure
- Integrates with live propagator service at ci.cedana.ai/v1
- Uses GET /v1/cluster endpoint to retrieve existing cluster IDs for testing
- Uses production Helm chart from OCI registry
- k3s cluster runs inside the Docker container
- Implements proper cleanup to avoid resource leaks within container
- Supports parallel test execution with unique container names
- Leverages existing test infrastructure and containerd setup
- CI builds and runs Docker container for each test execution
- Falls back to placeholder cluster ID if no clusters are available 