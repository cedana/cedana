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

export CLUSTER_NAME="${CLUSTER_NAME:-}"
export SLURM_CLUSTER_ID="${SLURM_CLUSTER_ID:-}"

setup_suite() {
    check_env CEDANA_URL
    check_env CEDANA_AUTH_TOKEN
    check_cmd docker

    # If SLURM_CLUSTER_ID is already set (e.g. from a prior workflow step), skip setup
    if [ -n "${SLURM_CLUSTER_ID:-}" ]; then
        debug_log "Cluster already provisioned (SLURM_CLUSTER_ID=$SLURM_CLUSTER_ID), skipping setup"

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
        return 0
    fi

    # Full setup when running outside CI or without a prior setup step
    if [ -z "${CEDANA_SLURM_DIR:-}" ]; then
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

    setup_slurm_cluster
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

    install_cedana_in_slurm
    setup_slurm_samples || return 1

    CLUSTER_NAME="test-slurm-$(unix_nano)"
    SLURM_CLUSTER_ID=$(register_slurm_cluster "$CLUSTER_NAME")
    export CLUSTER_NAME SLURM_CLUSTER_ID
    info_log "SLURM Cluster registered with ID: $SLURM_CLUSTER_ID"

    start_cedana_slurm_daemon
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
