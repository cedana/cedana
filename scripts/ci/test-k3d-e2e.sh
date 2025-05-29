#!/bin/bash

# K3d E2E Test Runner
# This script runs the k3d E2E tests with network fixes

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
    echo -e "${GREEN}[STEP]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_header() {
    echo -e "${BLUE}================================${NC}"
    echo -e "${BLUE}  K3d E2E Tests - Runner${NC}"
    echo -e "${BLUE}================================${NC}"
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
        echo
        echo "Your provided auth token is:"
        echo "  fa4318d1569bc89ac95c1223bbb41719e737640027c87200714204cb813de8a74546a5ec647052bcf19c507ca7013685"
        exit 1
    fi
    
    echo "✓ Environment variables are set"
}

run_k3d_tests() {
    print_step "Running K3d E2E tests..."
    
    cd "$REPO_ROOT"
    
    echo "Current directory: $(pwd)"
    echo "Running: make test-regression TAGS=e2e,k3d PARALLELISM=1"
    echo
    
    # Run the specific k3d tests
    if make test-regression TAGS=e2e,k3d PARALLELISM=1; then
        echo
        print_step "✅ K3d E2E tests completed successfully!"
    else
        local exit_code=$?
        echo
        print_error "❌ K3d E2E tests failed with exit code $exit_code"
        return $exit_code
    fi
}

run_docker_tests() {
    print_step "Running Docker environment tests..."
    
    cd "$REPO_ROOT"
    
    echo "Running: make test-regression TAGS=e2e,docker,simple PARALLELISM=1"
    echo
    
    # Run the simple docker tests first
    if make test-regression TAGS=e2e,docker,simple PARALLELISM=1; then
        echo
        print_step "✅ Docker tests completed successfully!"
    else
        local exit_code=$?
        echo
        print_error "❌ Docker tests failed with exit code $exit_code"
        return $exit_code
    fi
}

show_help() {
    cat << EOF
Usage: $0 [OPTIONS]

K3d E2E Test Runner

OPTIONS:
    -h, --help          Show this help message
    -d, --docker        Run Docker tests only
    -k, --k3d          Run K3d tests only
    -a, --all          Run both Docker and K3d tests (default)

ENVIRONMENT VARIABLES:
    CEDANA_URL          Required: Cedana API URL (e.g., https://ci.cedana.ai)
    CEDANA_AUTH_TOKEN   Required: Cedana authentication token

EXAMPLES:
    # Run all tests (docker + k3d)
    $0

    # Run only Docker tests
    $0 --docker

    # Run only K3d tests
    $0 --k3d

EOF
}

main() {
    local run_docker=false
    local run_k3d=false
    local run_all=true
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -d|--docker)
                run_docker=true
                run_all=false
                shift
                ;;
            -k|--k3d)
                run_k3d=true
                run_all=false
                shift
                ;;
            -a|--all)
                run_all=true
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
    
    check_env
    
    # Run tests based on options
    if [ "$run_all" = true ]; then
        run_docker_tests
        run_k3d_tests
    elif [ "$run_docker" = true ]; then
        run_docker_tests
    elif [ "$run_k3d" = true ]; then
        run_k3d_tests
    fi
    
    echo
    print_step "🎉 Test run completed!"
}

main "$@" 