#!/bin/bash

# Single Run E2E Test Script
# Avoids the duplicate test execution in the Makefile

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
    echo -e "${BLUE}  Single Run E2E Tests${NC}"
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
        exit 1
    fi
    
    echo "✓ Environment variables are set"
}

run_single_test() {
    local tags="$1"
    local parallelism="${2:-1}"
    
    print_step "Running E2E tests with tags: $tags"
    
    cd "$REPO_ROOT"
    
    # Run BATS directly to avoid the Makefile's double execution
    if [ -f /.dockerenv ]; then
        # We're inside a Docker container
        echo "Running inside Docker container..."
        if [ -n "$tags" ]; then
            bats --filter-tags "$tags" --jobs "$parallelism" -r test/regression
        else
            bats --jobs "$parallelism" -r test/regression
        fi
    else
        # We're outside Docker, need to run in container
        echo "Running in Docker container..."
        
        local env_args=(
            "-e" "CEDANA_URL=${CEDANA_URL}"
            "-e" "CEDANA_AUTH_TOKEN=${CEDANA_AUTH_TOKEN}"
        )
        
        if [ -n "${HF_TOKEN:-}" ]; then
            env_args+=("-e" "HF_TOKEN=${HF_TOKEN}")
        fi
        
        # Run with network host to fix connectivity
        docker run \
            --privileged --init --cgroupns=private --ipc=host \
            --network=host \
            -it --rm \
            -v "$PWD:/src:ro" \
            -w "/src" \
            "${env_args[@]}" \
            cedana/cedana-test:latest \
            bash -c "
                # Copy binaries to writable locations
                if [ -f ./cedana ]; then
                    cp ./cedana /usr/local/bin/cedana
                    chmod +x /usr/local/bin/cedana
                fi
                
                # Run the tests
                if [ -n '$tags' ]; then
                    bats --filter-tags '$tags' --jobs $parallelism -r test/regression
                else
                    bats --jobs $parallelism -r test/regression
                fi
            "
    fi
}

show_help() {
    cat << EOF
Usage: $0 [OPTIONS]

Single Run E2E Test Script (avoids duplicate execution)

OPTIONS:
    -h, --help              Show this help message
    -t, --tags TAGS         Test tags to run (default: e2e,real)
    -p, --parallelism N     Number of parallel jobs (default: 1)

EXAMPLES:
    # Run real pod C/R tests
    $0 --tags "e2e,real"

    # Run working demo tests
    $0 --tags "e2e,final,working"

    # Run simple Docker tests
    $0 --tags "e2e,docker,simple"

EOF
}

main() {
    local tags="e2e,real"
    local parallelism=1
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -t|--tags)
                tags="$2"
                shift 2
                ;;
            -p|--parallelism)
                parallelism="$2"
                shift 2
                ;;
            *)
                print_error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    print_header
    
    echo "Configuration:"
    echo "  Tags: $tags"
    echo "  Parallelism: $parallelism"
    echo
    
    check_env
    run_single_test "$tags" "$parallelism"
    
    echo
    print_step "🎉 Single run E2E test completed!"
}

main "$@" 