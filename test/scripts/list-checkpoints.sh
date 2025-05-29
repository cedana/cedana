#!/bin/bash

#######################################
### Cedana Checkpoint Listing Tool  ###
#######################################

set -e

# Script configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Default environment variables
DEFAULT_CEDANA_AUTH_TOKEN="fa4318d1569bc89ac95c1223bbb41719e737640027c87200714204cb813de8a74546a5ec647052bcf19c507ca7013685"
DEFAULT_CEDANA_URL="ci.cedana.ai/v1"

usage() {
    cat <<EOF
Usage: $0 [OPTIONS] [CLUSTER_ID]

List Cedana checkpoints via the propagator API.

OPTIONS:
    -h, --help              Show this help message
    --token TOKEN           Override CEDANA_AUTH_TOKEN
    --url URL               Override CEDANA_URL
    --raw                   Show raw JSON response
    --docker                Run inside Docker container (default)
    --local                 Run locally (requires propagator helpers in PATH)

ARGUMENTS:
    CLUSTER_ID              Optional cluster ID to filter checkpoints

EXAMPLES:
    $0                                          # List all checkpoints
    $0 my-cluster-id                           # List checkpoints for specific cluster
    $0 --raw                                   # Show raw JSON response
    $0 --token=mytoken --url=myurl            # Use custom credentials

ENVIRONMENT VARIABLES:
    CEDANA_AUTH_TOKEN       Cedana API authentication token
    CEDANA_URL              Cedana API base URL

EOF
}

# Functions
log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] $*" >&2
}

error() {
    echo "[ERROR] $*" >&2
    exit 1
}

list_checkpoints_docker() {
    local cluster_id="$1"
    local show_raw="$2"

    log "Listing checkpoints via Docker container..."

    # Prepare environment variables
    local env_args=()
    env_args+=("-e" "CEDANA_AUTH_TOKEN=${CEDANA_AUTH_TOKEN}")
    env_args+=("-e" "CEDANA_URL=${CEDANA_URL}")

    # Prepare Docker run arguments
    local docker_args=(
        "--rm"
        "--volume" "${REPO_ROOT}:/src"
        "--workdir" "/src"
    )

    # Add environment variables
    for env_arg in "${env_args[@]}"; do
        docker_args+=("$env_arg")
    done

    # Prepare command
    local cmd="source test/regression/helpers/propagator.bash && setup_propagator_auth"

    if [ "$show_raw" = "true" ]; then
        if [ -n "$cluster_id" ]; then
            cmd="$cmd && get_checkpoints '$cluster_id'"
        else
            cmd="$cmd && get_checkpoints"
        fi
    else
        if [ -n "$cluster_id" ]; then
            cmd="$cmd && list_checkpoints '$cluster_id'"
        else
            cmd="$cmd && list_checkpoints"
        fi
    fi

    # Run the container
    docker run "${docker_args[@]}" cedana-e2e-test:latest /bin/bash -c "$cmd"
}

list_checkpoints_local() {
    local cluster_id="$1"
    local show_raw="$2"

    log "Listing checkpoints locally..."

    # Source the helper functions
    if [ ! -f "$REPO_ROOT/test/regression/helpers/propagator.bash" ]; then
        error "Propagator helpers not found. Please run from the cedana repository root."
    fi

    export CEDANA_AUTH_TOKEN CEDANA_URL
    source "$REPO_ROOT/test/regression/helpers/propagator.bash"

    setup_propagator_auth

    if [ "$show_raw" = "true" ]; then
        get_checkpoints "$cluster_id"
    else
        list_checkpoints "$cluster_id"
    fi
}

main() {
    # Default values
    USE_DOCKER=true
    SHOW_RAW=false
    CLUSTER_ID=""

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
            --token)
                CEDANA_AUTH_TOKEN="$2"
                shift 2
                ;;
            --url)
                CEDANA_URL="$2"
                shift 2
                ;;
            --raw)
                SHOW_RAW=true
                shift
                ;;
            --docker)
                USE_DOCKER=true
                shift
                ;;
            --local)
                USE_DOCKER=false
                shift
                ;;
            -*)
                error "Unknown option: $1"
                ;;
            *)
                if [ -z "$CLUSTER_ID" ]; then
                    CLUSTER_ID="$1"
                else
                    error "Multiple cluster IDs specified"
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

    # Check Docker availability if using Docker mode
    if [ "$USE_DOCKER" = "true" ]; then
        if ! command -v docker &> /dev/null; then
            error "Docker is not installed or not in PATH"
        fi

        if ! docker info &> /dev/null; then
            error "Docker daemon is not running"
        fi

        # Check if Docker image exists
        if ! docker image inspect cedana-e2e-test:latest &> /dev/null; then
            log "Docker image not found, building it..."
            cd "$REPO_ROOT"
            docker build -t cedana-e2e-test:latest -f test/Dockerfile test/
        fi
    fi

    log "Cedana Checkpoint Listing Tool"
    log "URL: $CEDANA_URL"
    if [ -n "$CLUSTER_ID" ]; then
        log "Cluster ID: $CLUSTER_ID"
    else
        log "Listing all clusters"
    fi

    # List checkpoints
    if [ "$USE_DOCKER" = "true" ]; then
        list_checkpoints_docker "$CLUSTER_ID" "$SHOW_RAW"
    else
        list_checkpoints_local "$CLUSTER_ID" "$SHOW_RAW"
    fi
}

# Run main function
main "$@"
