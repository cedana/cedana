#!/bin/bash

#######################################
### Bare Metal K3s E2E Test Runner ###
#######################################

set -e

# Script configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

error() {
    echo "[ERROR] $*" >&2
    exit 1
}

cleanup() {
    echo "Performing cleanup on exit..."
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
}

run_k3s_test() {
    log "Starting K3s E2E checkpoint/restore test on bare metal"
    log "Cedana URL: $CEDANA_URL"

    cd "$REPO_ROOT"

    # Build local Cedana image first
    build_local_cedana_image

    # Export environment variables for the test
    export CEDANA_AUTH_TOKEN="$CEDANA_AUTH_TOKEN"
    export CEDANA_URL="$CEDANA_URL"

    # Export the local image tag for the test to use
    local image_tag=$(cat /tmp/cedana-local-image-tag)
    export CEDANA_LOCAL_HELPER_IMAGE="$image_tag"
    log "Using local helper image: $CEDANA_LOCAL_HELPER_IMAGE"

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

build_local_cedana_image() {
    log "Building local Cedana helper image with Pop!_OS support..."

    cd "$REPO_ROOT"

    # Build the local cedana image with a simple tag format
    local image_tag="cedana-helper-local:$(git rev-parse --short HEAD)"
    log "Building image: $image_tag"

    if ! docker build -t "$image_tag" -f Dockerfile .; then
        error "Failed to build local Cedana image"
    fi

    # Save the image tag for later use
    log "Built local Cedana image: $image_tag"
    echo "$image_tag" > /tmp/cedana-local-image-tag
}

main() {
    # Setup cleanup trap
    trap cleanup EXIT

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
