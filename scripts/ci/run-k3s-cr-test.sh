#!/bin/bash

# K3s Pod Checkpoint/Restore Test Runner
# Runs the k3s_pod_cr.bats test specifically

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

print_step() {
    echo -e "${GREEN}[K3S-CR]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_header() {
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}  K3s Pod Checkpoint/Restore E2E Test${NC}"
    echo -e "${BLUE}========================================${NC}"
    echo
}

check_env() {
    print_step "Checking environment variables..."
    
    local missing_vars=()
    
    if [ -z "${CEDANA_URL:-}" ]; then
        missing_vars+=("CEDANA_URL")
    fi
    
    if [ -z "${CEDANA_AUTH_TOKEN:-}" ]; then
        missing_vars+=("CEDANA_AUTH_TOKEN")
    fi
    
    if [ ${#missing_vars[@]} -gt 0 ]; then
        print_error "Missing required environment variables: ${missing_vars[*]}"
        echo
        echo "Please set these variables before running the test:"
        echo "  export CEDANA_URL=\"https://ci.cedana.ai\""
        echo "  export CEDANA_AUTH_TOKEN=\"your_token_here\""
        exit 1
    fi
    
    echo "✓ Environment variables are set"
    echo "  CEDANA_URL: ${CEDANA_URL}"
    echo "  AUTH_TOKEN: ${CEDANA_AUTH_TOKEN:0:10}..."
}

run_k3s_cr_test() {
    print_step "Running k3s pod checkpoint/restore test..."
    
    cd "$REPO_ROOT"
    
    # Check if we're in Docker or need to run Docker
    if [ -f /.dockerenv ]; then
        # We're inside a Docker container
        echo "Running inside Docker container..."
        run_test_directly
    else
        # We're outside Docker, need to run in container
        echo "Running in Docker container..."
        run_test_in_docker
    fi
}

run_test_directly() {
    # Ensure required directories exist
    mkdir -p /run/containerd/runc/k8s.io
    chmod 755 /run/containerd/runc/k8s.io
    
    # Run the specific k3s C/R test
    bats --filter-tags "k3s,checkpoint,restore" -v test/regression/e2e/k3s_pod_cr.bats
}

run_test_in_docker() {
    local env_args=(
        "-e" "CEDANA_URL=${CEDANA_URL}"
        "-e" "CEDANA_AUTH_TOKEN=${CEDANA_AUTH_TOKEN}"
    )
    
    if [ -n "${HF_TOKEN:-}" ]; then
        env_args+=("-e" "HF_TOKEN=${HF_TOKEN}")
    fi
    
    # Build test image if needed
    if ! docker image inspect cedana/cedana-test:latest >/dev/null 2>&1; then
        print_step "Building test Docker image..."
        docker build -f test/Dockerfile -t cedana/cedana-test:latest .
    fi
    
    # Run with network host to ensure connectivity
    docker run \
        --privileged --init --cgroupns=private --ipc=host \
        --network=host \
        -it --rm \
        -v "$PWD:/src:ro" \
        -w "/src" \
        "${env_args[@]}" \
        cedana/cedana-test:latest \
        bash -c "
            # Set up environment
            mkdir -p /run/containerd/runc/k8s.io
            chmod 755 /run/containerd/runc/k8s.io
            
            # Copy binaries to writable locations
            if [ -f ./cedana ]; then
                cp ./cedana /usr/local/bin/cedana
                chmod +x /usr/local/bin/cedana
            fi
            
            # Run the specific k3s C/R test
            echo 'Running k3s pod checkpoint/restore test...'
            bats --filter-tags 'k3s,checkpoint,restore' -v test/regression/e2e/k3s_pod_cr.bats
        "
}

show_help() {
    cat << EOF
Usage: $0 [OPTIONS]

K3s Pod Checkpoint/Restore Test Runner

This script runs the k3s_pod_cr.bats test specifically for testing pod
checkpoint and restore functionality via the Cedana Propagator API.

OPTIONS:
    -h, --help              Show this help message
    --local                 Force local execution (skip Docker)
    --docker                Force Docker execution
    --build                 Force rebuild of Docker test image

EXAMPLES:
    # Standard run (auto-detects environment)
    $0

    # Force local execution
    $0 --local

    # Force Docker execution with rebuild
    $0 --docker --build

ENVIRONMENT VARIABLES:
    CEDANA_URL              Propagator API URL (required)
    CEDANA_AUTH_TOKEN       Authentication token (required)
    HF_TOKEN               Hugging Face token (optional)

EOF
}

main() {
    local force_local=false
    local force_docker=false
    local force_build=false
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            --local)
                force_local=true
                shift
                ;;
            --docker)
                force_docker=true
                shift
                ;;
            --build)
                force_build=true
                shift
                ;;
            *)
                print_error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    print_header
    
    echo "Target test: test/regression/e2e/k3s_pod_cr.bats"
    echo "Test tags: k3s,checkpoint,restore"
    echo
    
    check_env
    
    if [ "$force_build" = true ] && [ "$force_local" = false ]; then
        print_step "Force rebuilding Docker test image..."
        docker build -f test/Dockerfile -t cedana/cedana-test:latest .
    fi
    
    if [ "$force_local" = true ]; then
        echo "Forcing local execution..."
        run_test_directly
    elif [ "$force_docker" = true ]; then
        echo "Forcing Docker execution..."
        run_test_in_docker
    else
        run_k3s_cr_test
    fi
    
    echo
    print_step "🎉 K3s pod checkpoint/restore test completed!"
}

main "$@" 