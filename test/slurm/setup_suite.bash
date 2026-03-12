#!/bin/bash

################################################################################
# Setup and Configuration
################################################################################

export CEDANA_CHECKPOINT_DIR=${CEDANA_CHECKPOINT_DIR:-cedana://ci}
export CEDANA_CHECKPOINT_COMPRESSION=${CEDANA_CHECKPOINT_COMPRESSION:-lz4}

source "${BATS_TEST_DIRNAME}"/../helpers/utils.bash
source "${BATS_TEST_DIRNAME}"/../helpers/daemon.bash
source "${BATS_TEST_DIRNAME}"/../helpers/slurm.bash
source "${BATS_TEST_DIRNAME}"/../helpers/slurm_propagator.bash
source "${BATS_TEST_DIRNAME}"/../helpers/metrics.bash

# Generate cluster name if not provided
if [ -z "${CLUSTER_NAME:-}" ]; then
    CLUSTER_NAME="test-slurm-$(unix_nano)"
fi
export CLUSTER_NAME
export SLURM_CLUSTER_ID="${SLURM_CLUSTER_ID:-}"

setup_suite() {
    check_env CEDANA_URL
    check_env CEDANA_AUTH_TOKEN

    # Validate docker is available
    check_cmd docker

    # Validate required cedana-slurm directory
    if [ -z "${CEDANA_SLURM_DIR:-}" ]; then
        # Auto-detect cedana-slurm directory
        if [ -d "../cedana-slurm" ]; then
            export CEDANA_SLURM_DIR="../cedana-slurm"
        elif [ -d "/cedana-slurm" ]; then
            export CEDANA_SLURM_DIR="/cedana-slurm"
        elif [ -d "${GITHUB_WORKSPACE:-}/cedana-slurm" ]; then
            export CEDANA_SLURM_DIR="${GITHUB_WORKSPACE}/cedana-slurm"
        else
            error_log "CEDANA_SLURM_DIR not set and cedana-slurm not found"
            return 1
        fi
    fi

    # Setup SLURM cluster
    setup_slurm_cluster

    # Setup SLURM accounting (MySQL + slurmdbd)
    setup_slurm_accounting

    if [ -z "${SLURM_SAMPLES_DIR:-}" ]; then
        if [ -d "../cedana-samples/slurm" ]; then
            export SLURM_SAMPLES_DIR="../cedana-samples/slurm"
        elif [ -d "/cedana-samples/slurm" ]; then
            export SLURM_SAMPLES_DIR="/cedana-samples/slurm"
        elif [ -d "/tmp/cedana-samples/slurm" ]; then
            export SLURM_SAMPLES_DIR="/tmp/cedana-samples/slurm"
        elif [ -d "${GITHUB_WORKSPACE:-}/cedana-samples/slurm" ]; then
            export SLURM_SAMPLES_DIR="${GITHUB_WORKSPACE}/cedana-samples/slurm"
        else
            export SLURM_SAMPLES_DIR="/data/cedana-samples/slurm"
        fi
    fi
    debug_log "Using SLURM_SAMPLES_DIR: $SLURM_SAMPLES_DIR"
    info_log "SLURM_SAMPLES_DIR contents:"
    ls -la "$SLURM_SAMPLES_DIR" 2>&1 | head -10 >&3 || true

    # Setup samples (clone cedana-samples if needed)
    setup_slurm_samples || return 1

    # Register cluster with propagator (unless already provided)
    if [ -z "$SLURM_CLUSTER_ID" ]; then
        debug_log "Registering SLURM cluster '$CLUSTER_NAME' with propagator..."
        SLURM_CLUSTER_ID=$(register_slurm_cluster "$CLUSTER_NAME")
        export SLURM_CLUSTER_ID
        info_log "======================================="
        info_log "SLURM Cluster registered with ID: $SLURM_CLUSTER_ID"
        info_log "======================================="
    else
        debug_log "Using provided SLURM cluster ID: $SLURM_CLUSTER_ID"
    fi

    # Install cedana + plugins into the SLURM cluster
    install_cedana_in_slurm

    # Start cedana-slurm daemon on the controller
    start_cedana_slurm_daemon

    # Validate propagator connectivity
    validate_slurm_propagator || return 1

    debug_log "SLURM test suite setup complete"
}

teardown_suite() {
    # Deregister cluster (only if we registered it)
    if [ -n "${SLURM_CLUSTER_ID:-}" ] && [ -z "${SLURM_CLUSTER_ID_PROVIDED:-}" ]; then
        deregister_slurm_cluster "$SLURM_CLUSTER_ID" || true
    fi

    # Tear down the SLURM cluster
    teardown_slurm_cluster
}
