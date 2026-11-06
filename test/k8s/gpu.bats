#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,gpu

load ../helpers/utils
load ../helpers/daemon # required for config env vars
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

setup_file() {
    if [ "${GPU:-0}" != "1" ]; then
        skip "GPU tests disabled (set GPU=1)"
    fi
}

#########
# Basic #
#########

# NOTE: Don't add too many tests here, as they will slow down
# the CI pipeline for every PR. Only basic sanity checks.

# bats test_tags=deploy
@test "Deploy a GPU pod" {
    local script
    local spec

    script='/cedana-samples/gpu_smr/mem-throughput-saxpy-loop'
    spec=$(cmd_pod_spec_gpu "$SAMPLES_IMAGE" "$script" 1)

    test_pod_spec DEPLOY "$spec" 600 5 60
}

# bats test_tags=dump
@test "Dump a GPU pod" {
    local script
    local spec

    script='/cedana-samples/gpu_smr/mem-throughput-saxpy-loop'
    spec=$(cmd_pod_spec_gpu "$SAMPLES_IMAGE" "$script" 1)

    test_pod_spec DEPLOY_DUMP "$spec" 600 5 60
}

# bats test_tags=restore
@test "Restore a GPU pod" {
    local script
    local spec

    script='/cedana-samples/gpu_smr/mem-throughput-saxpy-loop'
    spec=$(cmd_pod_spec_gpu "$SAMPLES_IMAGE" "$script" 1)

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 600 5 60
}

# bats test_tags=restore,crcr
@test "Dump/Restore/Dump/Restore a GPU pod" {
    local script
    local spec

    script='/cedana-samples/gpu_smr/mem-throughput-saxpy-loop'
    spec=$(cmd_pod_spec_gpu "$SAMPLES_IMAGE" "$script" 1)

    test_pod_spec DEPLOY_DUMP_RESTORE_DUMP_RESTORE "$spec" 600 5 60
}

###########
# Cleanup #
###########

# PIDs of the processes holding a GPU context on this node
gpu_compute_pids() {
    nvidia-smi --query-compute-apps=pid --format=csv,noheader | sort
}

# Pods this file's tests created, deleted even when a test fails
TEST_PODS=()

teardown() {
    for pod in "${TEST_PODS[@]}"; do
        kubectl delete pod "$pod" -n "$NAMESPACE" --force --grace-period=0 --wait=false &>/dev/null || true
    done
    TEST_PODS=()
    if [ -n "${CHECKPOINT_DIR_OVERRIDE:-}" ]; then
        rm -rf "$CHECKPOINT_DIR_OVERRIDE"
    fi
}

# A restore that fails must not leave a GPU controller or its workers holding the GPU.
# bats test_tags=restore,cleanup
@test "A failed GPU restore leaves nothing holding the GPU" {
    # The checkpoint must be on this host, to corrupt it, and nvidia-smi must see the GPU
    if [ "$PROVIDER" != "k3s" ] || [[ "$CEDANA_CHECKPOINT_DIR" != /* ]]; then
        skip "needs local checkpoint storage on a K3s node"
    fi
    local budget=120

    local baseline
    baseline=$(gpu_compute_pids)

    local script spec name
    script='/cedana-samples/gpu_smr/mem-throughput-saxpy-loop'
    spec=$(cmd_pod_spec_gpu "$SAMPLES_IMAGE" "$script" 1)
    kubectl apply -f "$spec"
    name=$(get_created_pod "$spec" "$NAMESPACE" 60)
    TEST_PODS+=("$name")
    validate_pod "$name" 600
    wait_for_cmd 60 "[ -n \"\$(comm -13 <(echo \"$baseline\") <(gpu_compute_pids))\" ]"

    CHECKPOINT_DIR_OVERRIDE="$CEDANA_CHECKPOINT_DIR/cedana-gpu-cleanup-$(unix_nano)"
    mkdir -p "$CHECKPOINT_DIR_OVERRIDE"
    local pod_id action_id
    pod_id=$(get_pod_id "$name" "$NAMESPACE")
    action_id=$(checkpoint_pod "$pod_id" "$CHECKPOINT_DIR_OVERRIDE")
    validate_action_id "$action_id"
    poll_action_status "$action_id" "checkpoint" 600

    # Cut every file of the checkpoint in half
    find "$CHECKPOINT_DIR_OVERRIDE" -type f -exec sh -c 'truncate -s $(($(stat -c %s "$1") / 2)) "$1"' _ {} \;

    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true

    local restore_id restored status=""
    restore_id=$(restore_pod "$action_id" "$CLUSTER_ID")
    validate_action_id "$restore_id"
    restored=$(wait_for_cmd 120 get_restored_pod "$name" "$NAMESPACE") || true
    if [ -n "$restored" ]; then TEST_PODS+=("$restored"); fi

    local start=$SECONDS
    while [ $((SECONDS - start)) -lt 600 ]; do
        status=$(get_restore_status "$restore_id")
        case "$status" in
            success)
                error_log "Restore $restore_id of a corrupted checkpoint reported success"
                return 1
                ;;
            fallback | error | failed) break ;;
        esac
        sleep 5
    done
    case "$status" in
        fallback | error | failed) debug_log "Restore of the corrupted checkpoint ended as '$status'" ;;
        *)
            error_log "Restore $restore_id of a corrupted checkpoint didn't end in 600s (status '${status:-not recorded}')"
            return 1
            ;;
    esac

    if [ -n "$restored" ]; then
        kubectl delete pod "$restored" -n "$NAMESPACE" --wait=true
    fi

    if ! wait_for_cmd "$budget" "[ -z \"\$(comm -13 <(echo \"$baseline\") <(gpu_compute_pids))\" ]"; then
        error_log "Processes still hold the GPU ${budget}s after the failed restore's pods were deleted:"
        for pid in $(comm -13 <(echo "$baseline") <(gpu_compute_pids)); do
            ps -o pid,ppid,etime,args -p "$pid" >&2 || true
        done
        return 1
    fi
}

##################
# Cedana Samples #
##################

# bats test_tags=dump,restore,samples
@test "Dump/Restore: CUDA Vector Addition" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-vector-add.yaml")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 600 5 60
}

# bats test_tags=dump,restore,samples,multicontainer
@test "Dump/Restore: CUDA Multi-container Vector Addition" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/gpu/cuda-vector-add-multicontainer.yaml")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 600 5 60
}
