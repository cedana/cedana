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

# Resolve SLURM_SAMPLES_DIR to the first sibling checkout that exists.
_resolve_slurm_samples_dir() {
    [ -n "${SLURM_SAMPLES_DIR:-}" ] && return 0
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
}

setup_suite() {
    check_env CEDANA_URL
    check_env CEDANA_AUTH_TOKEN

    # A reuse run (a later test step against a cluster an earlier step already
    # stood up) sets this explicitly; skip all provisioning.
    if [ -n "${SLURM_CLUSTER_ID_PROVIDED:-}" ]; then
        debug_log "Cluster already provisioned (SLURM_CLUSTER_ID=${SLURM_CLUSTER_ID:-}), skipping setup"
        check_cmd docker
        _resolve_slurm_samples_dir
        return 0
    fi

    # provision_slurm_host installs the Docker CLI when the image lacks it, so
    # the docker check follows it.
    provision_slurm_host || return 1
    check_cmd docker

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
    if [ "${SLURM_SETUP_ACCOUNTING:-1}" = "1" ]; then
        setup_slurm_accounting
    fi

    _resolve_slurm_samples_dir

    install_cedana_in_slurm

    # The ansible smoke job stands up only the cluster + plugins; it never
    # registers with the propagator or runs the cedana-slurm daemon.
    if [ "${SLURM_SETUP_REGISTER:-1}" != "1" ]; then
        export SLURM_CLUSTER_ID="${SLURM_CLUSTER_ID:-${SLURM_CLUSTER_ID_DEFAULT:-slurm-local-test}}"
        info_log "Skipping propagator registration (SLURM_SETUP_REGISTER=0); using SLURM_CLUSTER_ID=$SLURM_CLUSTER_ID"
        debug_log "SLURM test suite setup complete"
        return 0
    fi

    setup_slurm_samples || return 1

    CLUSTER_NAME="test-slurm-$(unix_nano)"
    SLURM_CLUSTER_ID=$(register_slurm_cluster "$CLUSTER_NAME")
    export CLUSTER_NAME SLURM_CLUSTER_ID
    info_log "SLURM Cluster registered with ID: $SLURM_CLUSTER_ID"

    # Hand the cluster to later steps in the same CI job: they inherit these as
    # a reuse run and skip provisioning.
    if [ -n "${GITHUB_ENV:-}" ]; then
        {
            echo "SLURM_CLUSTER_ID=$SLURM_CLUSTER_ID"
            echo "CEDANA_CLUSTER_ID=$SLURM_CLUSTER_ID"
            echo "CLUSTER_NAME=$CLUSTER_NAME"
            echo "SLURM_CLUSTER_ID_PROVIDED=1"
        } >>"$GITHUB_ENV"
    fi

    if [ "${SLURM_UNPRIVILEGED:-0}" = "1" ]; then
        setup_slurm_unprivileged_user
        start_cedana_slurm_daemon
        restart_cedana_slurm_daemon_unprivileged
    else
        start_cedana_slurm_daemon
    fi
    validate_slurm_propagator || return 1

    debug_log "SLURM test suite setup complete"
}

teardown_suite() {
    # Ephemeral CI runners keep the cluster for the job's later test steps and
    # are discarded wholesale; only a local run tears down.
    if [ -n "${SLURM_KEEP_CLUSTER:-}" ] || [ -n "${SLURM_CLUSTER_ID_PROVIDED:-}" ]; then
        return 0
    fi

    if [ -n "${SLURM_CLUSTER_ID:-}" ]; then
        deregister_slurm_cluster "$SLURM_CLUSTER_ID" || true
    fi
    teardown_slurm_cluster
}
