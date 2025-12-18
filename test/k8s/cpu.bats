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

    test_pod_spec DUMP "$spec"
}

# bats test_tags=restore
@test "Restore a pod" {
    local script
    local spec

    script=$(cat "$WORKLOADS"/date-loop.sh)
    spec=$(cmd_pod_spec "alpine:latest" "$script")

    test_pod_spec RESTORE "$spec"
}

################
# Sample-Based #
################

# bats test_tags=dump,restore
@test "Dump/Restore: Timestamp Logger" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cpu/counting.yaml")

    test_pod_spec DUMP_RESTORE "$spec"
}

# bats test_tags=dump,restore
@test "Dump/Restore: Multi-container Counter" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cpu/counting-multicontainer.yaml")

    test_pod_spec DUMP_RESTORE "$spec"
}
