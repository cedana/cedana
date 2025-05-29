#!/bin/bash

# Setup Test Environment
# This script helps set up the environment variables needed for E2E testing

set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_header() {
    echo -e "${BLUE}================================${NC}"
    echo -e "${BLUE}  Cedana E2E Test Environment${NC}"
    echo -e "${BLUE}================================${NC}"
    echo
}

print_step() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[NOTE]${NC} $1"
}

show_current_env() {
    echo "Current environment variables:"
    echo
    
    if [ -n "${CEDANA_URL:-}" ]; then
        echo "✅ CEDANA_URL: $CEDANA_URL"
    else
        echo "❌ CEDANA_URL: (not set)"
    fi
    
    if [ -n "${CEDANA_AUTH_TOKEN:-}" ]; then
        echo "✅ CEDANA_AUTH_TOKEN: ${CEDANA_AUTH_TOKEN:0:20}... (${#CEDANA_AUTH_TOKEN} chars)"
    else
        echo "❌ CEDANA_AUTH_TOKEN: (not set)"
    fi
    
    if [ -n "${HF_TOKEN:-}" ]; then
        echo "✅ HF_TOKEN: ${HF_TOKEN:0:20}... (${#HF_TOKEN} chars)"
    else
        echo "ℹ️  HF_TOKEN: (not set - optional)"
    fi
    
    echo
}

setup_env() {
    print_step "Setting up environment variables..."
    echo
    
    # Set the standard values
    export CEDANA_URL="https://ci.cedana.ai"
    export CEDANA_AUTH_TOKEN="fa4318d1569bc89ac95c1223bbb41719e737640027c87200714204cb813de8a74546a5ec647052bcf19c507ca7013685"
    
    echo "Environment variables have been set:"
    echo "  CEDANA_URL=$CEDANA_URL"
    echo "  CEDANA_AUTH_TOKEN=$CEDANA_AUTH_TOKEN"
    echo
    
    print_warning "These variables are only set for this shell session."
    print_warning "To make them persistent, add them to your ~/.bashrc or ~/.zshrc:"
    echo
    echo "  echo 'export CEDANA_URL=\"$CEDANA_URL\"' >> ~/.bashrc"
    echo "  echo 'export CEDANA_AUTH_TOKEN=\"$CEDANA_AUTH_TOKEN\"' >> ~/.bashrc"
    echo
}

run_quick_test() {
    print_step "Running a quick connectivity test..."
    echo
    
    if command -v curl >/dev/null 2>&1; then
        local test_url="${CEDANA_URL}/v1/cluster"
        echo "Testing: curl -s -H 'Authorization: Bearer \$CEDANA_AUTH_TOKEN' '$test_url'"
        
        local response
        if response=$(curl -s -w "%{http_code}" -H "Authorization: Bearer $CEDANA_AUTH_TOKEN" "$test_url" 2>/dev/null); then
            local status_code="${response: -3}"
            local body="${response%???}"
            
            echo "Response code: $status_code"
            
            if [ "$status_code" = "200" ]; then
                echo "✅ API connectivity test successful!"
                if command -v jq >/dev/null 2>&1 && [ -n "$body" ]; then
                    echo "Available clusters:"
                    echo "$body" | jq -r '.clusters[]?.id // "No clusters found"' 2>/dev/null || echo "$body"
                fi
            else
                echo "⚠️  API returned status code $status_code"
                echo "Response: $body"
            fi
        else
            echo "❌ Failed to connect to API"
        fi
    else
        echo "curl not available, skipping connectivity test"
    fi
    echo
}

show_next_steps() {
    print_step "Next steps:"
    echo
    echo "You can now run the E2E tests using one of these commands:"
    echo
    echo "1. Quick K3s tests only:"
    echo "   ./scripts/ci/test-k3s-e2e.sh --k3s"
    echo
    echo "2. Simple Docker tests only:"
    echo "   ./scripts/ci/test-k3s-e2e.sh --simple"
    echo
    echo "3. All E2E tests:"
    echo "   ./scripts/ci/test-k3s-e2e.sh"
    echo
    echo "4. Full CI simulation (Docker container):"
    echo "   ./scripts/ci/local-e2e-test.sh"
    echo
    echo "5. Using Makefile directly:"
    echo "   make test-regression TAGS=e2e,k3s PARALLELISM=1"
    echo
}

show_help() {
    cat << EOF
Usage: $0 [OPTIONS]

Setup Test Environment - Configure environment variables for E2E testing

OPTIONS:
    -h, --help          Show this help message
    -s, --setup         Set up environment variables (default)
    -c, --check         Check current environment variables
    -t, --test          Run quick API connectivity test
    -q, --quiet         Quiet mode (less output)

EXAMPLES:
    # Set up environment and test connectivity
    $0

    # Just check current environment
    $0 --check

    # Set up and run connectivity test
    $0 --setup --test

EOF
}

main() {
    local do_setup=true
    local do_check=false
    local do_test=false
    local quiet=false
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -s|--setup)
                do_setup=true
                shift
                ;;
            -c|--check)
                do_check=true
                do_setup=false
                shift
                ;;
            -t|--test)
                do_test=true
                shift
                ;;
            -q|--quiet)
                quiet=true
                shift
                ;;
            *)
                echo "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    if [ "$quiet" = false ]; then
        print_header
    fi
    
    if [ "$do_check" = true ] || [ "$do_setup" = false ]; then
        show_current_env
    fi
    
    if [ "$do_setup" = true ]; then
        setup_env
    fi
    
    if [ "$do_test" = true ]; then
        run_quick_test
    fi
    
    if [ "$quiet" = false ] && [ "$do_setup" = true ]; then
        show_next_steps
    fi
}

# If script is sourced, just set up environment without running main
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
else
    # Script is being sourced, just set up the environment
    export CEDANA_URL="https://ci.cedana.ai"
    export CEDANA_AUTH_TOKEN="fa4318d1569bc89ac95c1223bbb41719e737640027c87200714204cb813de8a74546a5ec647052bcf19c507ca7013685"
    echo "Environment variables set for current shell session"
fi 