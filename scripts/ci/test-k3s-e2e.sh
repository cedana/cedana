#!/bin/bash

# Quick K3s E2E Test Runner
# This script runs just the k3s E2E tests using the existing Makefile

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
    echo -e "${BLUE}  K3s E2E Tests - Quick Runner${NC}"
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

run_k3s_tests() {
    print_step "Running K3s E2E tests..."
    
    cd "$REPO_ROOT"
    
    echo "Current directory: $(pwd)"
    echo "Running: make test-regression TAGS=e2e,k3s PARALLELISM=1"
    echo
    
    # Run the specific k3s tests
    if make test-regression TAGS=e2e,k3s PARALLELISM=1; then
        echo
        print_step "✅ K3s E2E tests completed successfully!"
    else
        local exit_code=$?
        echo
        print_error "❌ K3s E2E tests failed with exit code $exit_code"
        return $exit_code
    fi
}

run_simple_docker_tests() {
    print_step "Running simple Docker environment tests..."
    
    cd "$REPO_ROOT"
    
    echo "Running: make test-regression TAGS=e2e,docker,simple PARALLELISM=1"
    echo
    
    # Run the simple docker tests first
    if make test-regression TAGS=e2e,docker,simple PARALLELISM=1; then
        echo
        print_step "✅ Simple Docker tests completed successfully!"
    else
        local exit_code=$?
        echo
        print_error "❌ Simple Docker tests failed with exit code $exit_code"
        return $exit_code
    fi
}

show_help() {
    cat << EOF
Usage: $0 [OPTIONS]

Quick K3s E2E Test Runner

OPTIONS:
    -h, --help          Show this help message
    -s, --simple        Run simple Docker tests only
    -k, --k3s          Run K3s tests only
    -a, --all          Run both simple and K3s tests (default)

ENVIRONMENT VARIABLES:
    CEDANA_URL          Required: Cedana API URL (e.g., https://ci.cedana.ai)
    CEDANA_AUTH_TOKEN   Required: Cedana authentication token

EXAMPLES:
    # Run all tests (simple + k3s)
    $0

    # Run only simple Docker tests
    $0 --simple

    # Run only K3s tests
    $0 --k3s

EOF
}

main() {
    local run_simple=false
    local run_k3s=false
    local run_all=true
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -s|--simple)
                run_simple=true
                run_all=false
                shift
                ;;
            -k|--k3s)
                run_k3s=true
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
        run_simple_docker_tests
        run_k3s_tests
    elif [ "$run_simple" = true ]; then
        run_simple_docker_tests
    elif [ "$run_k3s" = true ]; then
        run_k3s_tests
    fi
    
    echo
    print_step "🎉 Test run completed!"
}

main "$@" 