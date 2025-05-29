#!/bin/bash

# Local E2E CI Test Runner
# This script replicates the GitHub Actions E2E regression test workflow locally

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
DOCKER_IMAGE="cedana/cedana-test:latest"
CONTAINER_NAME="cedana-e2e-test-$$"

# Default values (can be overridden by environment variables)
PARALLELISM="${PARALLELISM:-4}"
LARGE="${LARGE:-false}"
DEBUG="${DEBUG:-false}"
TAGS="${TAGS:-e2e}"
ARCH="${ARCH:-$(uname -m | sed 's/x86_64/amd64/')}"

# Environment variables that should be passed to container
ENV_VARS=(
    "CEDANA_URL"
    "CEDANA_AUTH_TOKEN"
    "HF_TOKEN"
    "PARALLELISM"
    "LARGE"
    "TAGS"
)

print_header() {
    echo -e "${BLUE}================================${NC}"
    echo -e "${BLUE}  Cedana E2E CI Local Test${NC}"
    echo -e "${BLUE}================================${NC}"
    echo
}

print_step() {
    echo -e "${GREEN}[STEP]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_prerequisites() {
    print_step "Checking prerequisites..."
    
    # Check if docker is available
    if ! command -v docker >/dev/null 2>&1; then
        print_error "Docker is not installed or not in PATH"
        exit 1
    fi
    
    # Check if we're in the right directory
    if [ ! -f "$REPO_ROOT/Makefile" ] || [ ! -d "$REPO_ROOT/test" ]; then
        print_error "Script must be run from cedana repository root or scripts/ci directory"
        exit 1
    fi
    
    # Check if required environment variables are set
    local missing_vars=()
    for var in CEDANA_URL CEDANA_AUTH_TOKEN; do
        if [ -z "${!var:-}" ]; then
            missing_vars+=("$var")
        fi
    done
    
    if [ ${#missing_vars[@]} -gt 0 ]; then
        print_error "Missing required environment variables: ${missing_vars[*]}"
        echo "Please set these variables before running the test:"
        for var in "${missing_vars[@]}"; do
            echo "  export $var=your_value"
        done
        exit 1
    fi
    
    echo "✓ Prerequisites check passed"
}

pull_docker_image() {
    print_step "Pulling Docker test image..."
    
    if docker pull "$DOCKER_IMAGE"; then
        echo "✓ Docker image pulled successfully"
    else
        print_warning "Failed to pull $DOCKER_IMAGE, will try to use local image"
    fi
}

build_cedana_binary() {
    print_step "Building Cedana binary..."
    
    cd "$REPO_ROOT"
    
    # Build the binary
    if make build; then
        echo "✓ Cedana binary built successfully"
    else
        print_error "Failed to build Cedana binary"
        exit 1
    fi
    
    # Make it executable
    chmod +x "./cedana"
    echo "✓ Cedana binary is executable"
}

build_criu_binary() {
    print_step "Checking for CRIU binary..."
    
    # For local testing, we'll assume CRIU is available in the container
    # or we can download it separately if needed
    if command -v criu >/dev/null 2>&1; then
        echo "✓ CRIU is available locally"
        # Copy local criu for mounting
        cp "$(which criu)" "$REPO_ROOT/criu" || true
    else
        print_warning "CRIU not found locally, will rely on container installation"
        # Create a dummy file to avoid mount errors
        touch "$REPO_ROOT/criu"
    fi
}

run_e2e_tests() {
    print_step "Running E2E tests in container..."
    
    cd "$REPO_ROOT"
    
    # Build environment variables for docker run
    local env_args=()
    for var in "${ENV_VARS[@]}"; do
        if [ -n "${!var:-}" ]; then
            env_args+=("-e" "$var=${!var}")
        fi
    done
    
    # Prepare the docker run command
    local docker_args=(
        "run"
        "--name" "$CONTAINER_NAME"
        "--privileged"
        "--init"
        "--cgroupns=private"
        "--ipc=host"
        "--network=host"
        "-it"
        "--rm"
        "-v" "$PWD:/src:ro"
        "-w" "/src"
    )
    
    # Add environment variables
    docker_args+=("${env_args[@]}")
    
    # Add the image
    docker_args+=("$DOCKER_IMAGE")
    
    # The command to run inside the container
    local container_cmd="
        set -euo pipefail
        
        echo 'Setting up container environment...'
        
        # Copy binaries to writable locations
        if [ -f ./cedana ]; then
            cp ./cedana /usr/local/bin/cedana
            chmod +x /usr/local/bin/cedana
            echo 'Cedana binary copied and ready'
        else
            echo 'Warning: Cedana binary not found'
        fi
        
        if [ -f ./criu ]; then
            cp ./criu /usr/local/bin/criu
            chmod +x /usr/local/bin/criu
            echo 'CRIU binary copied and ready'
        fi
        
        # Install CRIU plugin if available
        if command -v cedana >/dev/null 2>&1; then
            echo 'Installing CRIU plugin...'
            cedana plugin install criu || echo 'CRIU plugin install failed'
        fi
        
        # Mark git directory as safe
        git config --global --add safe.directory \"\$(pwd)\"
        
        echo 'Running E2E regression tests...'
        echo \"Parallelism: \$PARALLELISM\"
        echo \"Tags: \$TAGS\"
        echo \"Large tests: \$LARGE\"
        
        # Run the tests
        if [ \"\$LARGE\" = \"true\" ]; then
            make test-regression TAGS=\$TAGS PARALLELISM=\$PARALLELISM
        else
            make test-regression TAGS=\$TAGS,!large PARALLELISM=\$PARALLELISM
        fi
    "
    
    echo "Starting Docker container..."
    echo "Container name: $CONTAINER_NAME"
    echo "Tags: $TAGS"
    echo "Parallelism: $PARALLELISM"
    echo
    
    # Run the container
    if docker "${docker_args[@]}" bash -c "$container_cmd"; then
        echo
        print_step "E2E tests completed successfully!"
    else
        local exit_code=$?
        echo
        print_error "E2E tests failed with exit code $exit_code"
        return $exit_code
    fi
}

cleanup() {
    print_step "Cleaning up..."
    
    # Remove container if it exists
    if docker ps -a --format '{{.Names}}' | grep -q "^$CONTAINER_NAME$"; then
        docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
    fi
    
    # Clean up temporary files
    rm -f "$REPO_ROOT/criu" || true
    
    echo "✓ Cleanup completed"
}

show_help() {
    cat << EOF
Usage: $0 [OPTIONS]

Local E2E CI Test Runner - Replicates GitHub Actions E2E workflow

OPTIONS:
    -h, --help          Show this help message
    -t, --tags TAGS     Test tags to run (default: e2e)
    -p, --parallelism N Number of parallel jobs (default: 4)
    -l, --large         Include large tests (default: false)
    -d, --debug         Enable debug mode (default: false)
    --arch ARCH         Architecture (default: auto-detected)

ENVIRONMENT VARIABLES:
    CEDANA_URL          Required: Cedana API URL
    CEDANA_AUTH_TOKEN   Required: Cedana authentication token
    HF_TOKEN           Optional: Hugging Face token
    PARALLELISM        Override parallelism setting
    LARGE              Override large tests setting
    TAGS               Override tags setting

EXAMPLES:
    # Run all E2E tests
    $0

    # Run specific tags with custom parallelism
    $0 --tags "e2e,k3s" --parallelism 2

    # Run with large tests included
    $0 --large

    # Run only k3s tests
    $0 --tags "k3s"

EOF
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -t|--tags)
            TAGS="$2"
            shift 2
            ;;
        -p|--parallelism)
            PARALLELISM="$2"
            shift 2
            ;;
        -l|--large)
            LARGE="true"
            shift
            ;;
        -d|--debug)
            DEBUG="true"
            shift
            ;;
        --arch)
            ARCH="$2"
            shift 2
            ;;
        *)
            print_error "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Main execution
main() {
    print_header
    
    echo "Configuration:"
    echo "  Architecture: $ARCH"
    echo "  Tags: $TAGS"
    echo "  Parallelism: $PARALLELISM"
    echo "  Large tests: $LARGE"
    echo "  Debug: $DEBUG"
    echo
    
    # Set up cleanup trap
    trap cleanup EXIT
    
    # Run the workflow steps
    check_prerequisites
    pull_docker_image
    build_cedana_binary
    build_criu_binary
    run_e2e_tests
    
    echo
    print_step "🎉 Local E2E CI test completed successfully!"
}

# Run main function
main "$@" 