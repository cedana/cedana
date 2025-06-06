#!/bin/bash

#######################################
### Bare Metal K3s E2E Test Runner ###
#######################################

set -e

# Script configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Default values
CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-1d0e30662b9e998abb06f4e1db9362e5fea7b21337a5a98fb5e734b7f23555fa57a43abf33f2f65847a184de9ae77cf4}"
CEDANA_URL="${CEDANA_URL:-https://ci.cedana.ai/v1}"
DEBUG=false
CLEANUP=true

# Functions
usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Run K3s E2E checkpoint/restore test on bare metal.

OPTIONS:
    -h, --help              Show this help message
    --token TOKEN          Override CEDANA_AUTH_TOKEN
    --url URL              Override CEDANA_URL
    --debug                Enable debug output
    --no-cleanup           Don't clean up k3s after test (for debugging)

ENVIRONMENT VARIABLES:
    CEDANA_AUTH_TOKEN       Cedana API authentication token
    CEDANA_URL              Cedana API base URL

EXAMPLES:
    $0                                              # Run with default credentials
    $0 --token=mytoken --url=myurl                 # Run with custom credentials
    $0 --debug --no-cleanup                        # Debug mode with k3s preserved

PREREQUISITES:
    - curl (for downloading k3s and helm)
    - systemctl (for managing k3s service)
    - sudo access (required for k3s installation)
    - bats (for running tests)

EOF
}

log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] $*" >&2
}

debug() {
    if [ "$DEBUG" = "true" ]; then
        echo "[DEBUG] $*" >&2
    fi
}

error() {
    echo "[ERROR] $*" >&2
    exit 1
}

check_prerequisites() {
    local missing_deps=()
    
    # Check for required commands
    for cmd in curl sudo systemctl bats; do
        if ! command -v "$cmd" &> /dev/null; then
            missing_deps+=("$cmd")
        fi
    done
    
    if [ ${#missing_deps[@]} -ne 0 ]; then
        error "Missing required dependencies: ${missing_deps[*]}"
    fi
    
    # Check if running as root (we need sudo for k3s)
    if [ "$EUID" -eq 0 ]; then
        log "Warning: Running as root. Consider running as a regular user with sudo access."
    fi
    
    # Check sudo access
    if ! sudo -n true 2>/dev/null; then
        log "This script requires sudo access for k3s installation and management."
        log "You may be prompted for your password."
    fi
    
    log "Prerequisites check passed"
}

cleanup_on_exit() {
    if [ "$CLEANUP" = "true" ]; then
        log "Performing cleanup on exit..."
        cd "$REPO_ROOT"
        
        # Run the BATS teardown if it exists
        if [ -f "test/regression/e2e/k3s_pod_cr.bats" ]; then
            echo "Running BATS teardown..."
            # Just run a simple teardown - the teardown_file function will handle cleanup
        fi
        
        # Additional cleanup - force remove k3s if still present
        if [ -f /usr/local/bin/k3s-uninstall.sh ]; then
            log "Force cleaning up k3s..."
            sudo /usr/local/bin/k3s-uninstall.sh || true
        fi
    fi
}

run_k3s_test() {
    log "Starting K3s E2E checkpoint/restore test on bare metal"
    log "Cedana URL: $CEDANA_URL"
    
    cd "$REPO_ROOT"
    
    # Export environment variables for the test
    export CEDANA_AUTH_TOKEN="$CEDANA_AUTH_TOKEN"
    export CEDANA_URL="$CEDANA_URL"
    
    # Add debug flags if requested
    local bats_args=()
    if [ "$DEBUG" = "true" ]; then
        bats_args+=("--pretty")
        bats_args+=("--timing")
        bats_args+=("--no-tempdir-cleanup")
    fi
    
    log "Running BATS test: test/regression/e2e/k3s_pod_cr.bats"
    bats "${bats_args[@]}" test/regression/e2e/k3s_pod_cr.bats
    
    local test_exit_code=$?
    
    if [ $test_exit_code -eq 0 ]; then
        log "✅ K3s E2E test passed successfully!"
    else
        log "❌ K3s E2E test failed!"
    fi
    
    return $test_exit_code
}

main() {
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                usage
                exit 0
                ;;
            --token)
                CEDANA_AUTH_TOKEN="$2"
                shift 2
                ;;
            --url)
                CEDANA_URL="$2"
                shift 2
                ;;
            --debug)
                DEBUG=true
                set -x
                shift
                ;;
            --no-cleanup)
                CLEANUP=false
                shift
                ;;
            -*)
                error "Unknown option: $1"
                ;;
            *)
                error "Unexpected argument: $1"
                ;;
        esac
    done
    
    # Validate required environment variables
    if [ -z "$CEDANA_AUTH_TOKEN" ]; then
        error "CEDANA_AUTH_TOKEN is required"
    fi
    
    if [ -z "$CEDANA_URL" ]; then
        error "CEDANA_URL is required"
    fi
    
    # Check that we're not in a container
    if [ -f /.dockerenv ]; then
        error "This script is designed for bare metal execution. Detected Docker container environment."
    fi
    
    # Check prerequisites
    check_prerequisites
    
    # Setup cleanup trap
    trap cleanup_on_exit EXIT
    
    # Run the test
    run_k3s_test
    local test_exit_code=$?
    
    if [ $test_exit_code -eq 0 ]; then
        log "🎉 All tests passed successfully on bare metal!"
    else
        log "💥 Tests failed on bare metal!"
    fi
    
    exit $test_exit_code
}

# Run main function
main "$@" 