#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes

load ../helpers/utils
load ../helpers/daemon # required for config env vars
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

#########
# Basic #
#########

# bats test_tags=deploy
@test "Deploy a pod" {
    local script
    local spec

    script=$(cat "$WORKLOADS"/date-loop.sh)
    spec=$(cmd_pod_spec "alpine:latest" "$script")

    test_pod_spec DEPLOY "$spec"
}

# bats test_tags=dump
@test "Dump a pod" {
    local script
    local spec

    script=$(cat "$WORKLOADS"/date-loop.sh)
    spec=$(cmd_pod_spec "alpine:latest" "$script")

    test_pod_spec DEPLOY_DUMP "$spec"
}

# bats test_tags=restore
@test "Restore a pod" {
    local script
    local spec

    script=$(cat "$WORKLOADS"/date-loop.sh)
    spec=$(cmd_pod_spec "alpine:latest" "$script")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec"
}

# bats test_tags=restore,crcr
@test "Dump/Restore/Dump/Restore a pod" {
    local script
    local spec

    script=$(cat "$WORKLOADS"/date-loop.sh)
    spec=$(cmd_pod_spec "alpine:latest" "$script")

    test_pod_spec DEPLOY_DUMP_RESTORE_DUMP_RESTORE "$spec"
}

##################
# Cedana Samples #
##################

# bats test_tags=dump,restore,samples
@test "Dump/Restore: Timestamp Logger" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cpu/counting.yaml")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 120
}

# bats test_tags=dump,restore,samples
@test "Dump/Restore: Multi-container Counter" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cpu/counting-multicontainer.yaml")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 120
}
