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

# NOTE: Don't add too many tests here, as they will slow down
# the CI pipeline for every PR. Only basic sanity checks.

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

# bats test_tags=restore,dns
@test "Restore a pod (keeps DNS and hostname)" {
    local script
    local spec
    local name
    local restored

    script=$(cat "$WORKLOADS"/date-loop.sh)
    spec=$(cmd_pod_spec "alpine:latest" "$script")
    name=$(get_created_pod "$spec" "$NAMESPACE")

    kubectl apply -f "$spec"
    validate_pod "$name" 60
    validate_pod_dns_and_hostname "$name"

    local action_id
    action_id=$(checkpoint_pod "$(get_pod_id "$name" "$NAMESPACE")")
    validate_action_id "$action_id"
    poll_action_status "$action_id" "checkpoint" 60

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true

    restore_pod "$action_id" "$CLUSTER_ID"
    restored=$(wait_for_cmd 60 get_restored_pod "$name")
    validate_pod "$restored" 60

    run validate_pod_dns_and_hostname "$restored"
    echo "$output"
    kubectl delete pod "$restored" -n "$NAMESPACE" --wait=false
    [ "$status" -eq 0 ]
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

# bats test_tags=dump,restore,samples,multicontainer
@test "Dump/Restore: Multi-container Counter" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cpu/counting-multicontainer.yaml")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 120
}
