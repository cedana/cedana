#!/bin/bash

#######################################
### Docker E2E Test Runner Script  ###
#######################################

set -e

# Script configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
IMAGE_NAME="cedana-e2e-test"
IMAGE_TAG="${DOCKER_TAG:-latest}"
CONTAINER_NAME_PREFIX="cedana-e2e-test"

# Functions
usage() {
    cat <<EOF
Usage: $0 [OPTIONS] [TEST_FILE]

Run E2E tests in Docker container.

OPTIONS:
    -h, --help              Show this help message
    -b, --build             Force rebuild of Docker image
    -t, --tag TAG          Docker image tag (default: latest)
    -e, --env KEY=VALUE    Set environment variable for container
    --token TOKEN          Override CEDANA_AUTH_TOKEN
    --url URL              Override CEDANA_URL
    --cleanup              Clean up containers and images after test
    --no-cleanup           Don't clean up containers (for debugging)
    --privileged           Run container with privileged mode (required for k3s)
    --debug                Enable debug output

ARGUMENTS:
    TEST_FILE              Specific test file to run (default: all e2e tests)

EXAMPLES:
    $0 --build                                    # Build image and run all e2e tests
    $0 test/regression/e2e/k3s_pod_cr.bats      # Run specific test file
    $0 --token=mytoken --url=myurl               # Run with custom credentials
    $0 --debug --no-cleanup                      # Debug mode with container preserved

ENVIRONMENT VARIABLES:
    CEDANA_AUTH_TOKEN       Cedana API authentication token
    CEDANA_URL              Cedana API base URL
    DOCKER_TAG              Docker image tag to use

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

build_image() {
    log "Building Docker image: ${IMAGE_NAME}:${IMAGE_TAG}"

    cd "$REPO_ROOT"
    docker build -t "${IMAGE_NAME}:${IMAGE_TAG}" -f test/Dockerfile test/

    if [ $? -eq 0 ]; then
        log "Docker image built successfully"
    else
        error "Failed to build Docker image"
    fi
}

generate_container_name() {
    echo "${CONTAINER_NAME_PREFIX}-$(date +%s)-$$"
}

run_test_container() {
    local test_file="$1"
    local container_name
    container_name=$(generate_container_name)

    log "Starting test container: $container_name"

    # Prepare environment variables
    local env_args=()
    env_args+=("-e" "CEDANA_AUTH_TOKEN=${CEDANA_AUTH_TOKEN}")
    env_args+=("-e" "CEDANA_URL=${CEDANA_URL}")

    # Add any additional environment variables
    for env_var in "${EXTRA_ENV[@]}"; do
        env_args+=("-e" "$env_var")
    done

    # Prepare Docker run arguments
    local docker_args=(
        "--name" "$container_name"
        "--rm"
        "--volume" "${REPO_ROOT}:/src"
        "--workdir" "/src"
    )

    # Add privileged mode if requested (required for k3s)
    if [ "$PRIVILEGED" = "true" ]; then
        docker_args+=("--privileged")
        docker_args+=("--security-opt" "apparmor:unconfined")
        docker_args+=("--tmpfs" "/tmp")
        docker_args+=("--tmpfs" "/run")
        docker_args+=("--volume" "/sys/fs/cgroup:/sys/fs/cgroup:rw")
        docker_args+=("--cgroupns" "host")
    fi

    # Add environment variables
    for env_arg in "${env_args[@]}"; do
        docker_args+=("$env_arg")
    done

    # Prepare test command
    local test_cmd
    if [ -n "$test_file" ]; then
        test_cmd="bats $test_file"
    else
        test_cmd="bats test/regression/e2e/"
    fi

    debug "Docker args: ${docker_args[*]}"
    debug "Test command: $test_cmd"

    # Run the container
    log "Running test command: $test_cmd"
    docker run "${docker_args[@]}" "${IMAGE_NAME}:${IMAGE_TAG}" /bin/bash -c "
        set -e
        export PATH=\$PATH:/usr/local/bin

        # Wait a moment for any background services
        sleep 2

        # Run the test
        $test_cmd
    "

    local exit_code=$?

    if [ $exit_code -eq 0 ]; then
        log "Tests completed successfully"
    else
        log "Tests failed with exit code: $exit_code"
    fi

    return $exit_code
}

cleanup_docker_resources() {
    if [ "$CLEANUP" = "true" ]; then
        log "Cleaning up Docker resources..."

        # Remove containers
        local containers
        containers=$(docker ps -a --filter "name=${CONTAINER_NAME_PREFIX}" --format "{{.Names}}" 2>/dev/null || true)
        if [ -n "$containers" ]; then
            log "Removing containers: $containers"
            echo "$containers" | xargs docker rm -f 2>/dev/null || true
        fi

        # Remove images if requested
        if [ "$CLEANUP_IMAGES" = "true" ]; then
            log "Removing Docker image: ${IMAGE_NAME}:${IMAGE_TAG}"
            docker rmi "${IMAGE_NAME}:${IMAGE_TAG}" 2>/dev/null || true
        fi
    fi
}

main() {
    # Default values
    BUILD_IMAGE=false
    CLEANUP=true
    CLEANUP_IMAGES=false
    PRIVILEGED=true  # Default to privileged for k3s
    DEBUG=false
    EXTRA_ENV=()
    TEST_FILE=""

    # Environment variables with defaults
    CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-$DEFAULT_CEDANA_AUTH_TOKEN}"
    CEDANA_URL="${CEDANA_URL:-$DEFAULT_CEDANA_URL}"

    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                usage
                exit 0
                ;;
            -b|--build)
                BUILD_IMAGE=true
                shift
                ;;
            -t|--tag)
                IMAGE_TAG="$2"
                shift 2
                ;;
            -e|--env)
                EXTRA_ENV+=("$2")
                shift 2
                ;;
            --token)
                CEDANA_AUTH_TOKEN="$2"
                shift 2
                ;;
            --url)
                CEDANA_URL="$2"
                shift 2
                ;;
            --cleanup)
                CLEANUP=true
                CLEANUP_IMAGES=true
                shift
                ;;
            --no-cleanup)
                CLEANUP=false
                shift
                ;;
            --privileged)
                PRIVILEGED=true
                shift
                ;;
            --debug)
                DEBUG=true
                set -x
                shift
                ;;
            -*)
                error "Unknown option: $1"
                ;;
            *)
                if [ -z "$TEST_FILE" ]; then
                    TEST_FILE="$1"
                else
                    error "Multiple test files specified"
                fi
                shift
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

    # Check if Docker is available
    if ! command -v docker &> /dev/null; then
        error "Docker is not installed or not in PATH"
    fi

    # Check if Docker daemon is running
    if ! docker info &> /dev/null; then
        error "Docker daemon is not running"
    fi

    # Setup cleanup trap
    trap cleanup_docker_resources EXIT

    log "Starting E2E test run with Docker"
    log "Image: ${IMAGE_NAME}:${IMAGE_TAG}"
    log "Cedana URL: $CEDANA_URL"
    log "Test file: ${TEST_FILE:-all e2e tests}"

    # Build image if requested or if it doesn't exist
    if [ "$BUILD_IMAGE" = "true" ] || ! docker image inspect "${IMAGE_NAME}:${IMAGE_TAG}" &> /dev/null; then
        build_image
    else
        log "Using existing Docker image: ${IMAGE_NAME}:${IMAGE_TAG}"
    fi

    # Run the tests
    run_test_container "$TEST_FILE"
    local test_exit_code=$?

    if [ $test_exit_code -eq 0 ]; then
        log "All tests passed successfully!"
    else
        log "Tests failed!"
    fi

    exit $test_exit_code
}

# Run main function
main "$@"
