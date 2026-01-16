#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,chaos,stress,cpu

# Chaos/Stress Test for Cedana Checkpoint/Migrate/Restore
#
# This test exercises checkpoint/migrate/restore under realistic chaos conditions
# with randomly interleaved events:
#
# - Deploys multiple diverse workloads
# - Runs a chaos event loop that randomly triggers:
#   - Checkpoint a random running pod
#   - Delete a random node (simulating node failure)
#   - Restore from a completed checkpoint
# - Events happen at random intervals, interleaved together
# - Validates all workloads can be restored after chaos
#
# Environment variables:
#   CHAOS_DURATION                        - Total chaos duration in seconds (default: 120)
#   CHAOS_MIN_EVENT_INTERVAL              - Min seconds between events (default: 5)
#   CHAOS_MAX_EVENT_INTERVAL              - Max seconds between events (default: 15)
#   CHAOS_MIN_CHECKPOINTS_BEFORE_DELETE   - Min checkpoints before allowing node delete (default: 2)
#   CHAOS_MAX_NODE_DELETES                - Max nodes to delete (default: 2)
#   CHAOS_NAMESPACE                       - Namespace for chaos test pods (default: chaos-test)

load ../helpers/utils
load ../helpers/daemon
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

# Configuration
CHAOS_DURATION="${CHAOS_DURATION:-120}"
CHAOS_MIN_EVENT_INTERVAL="${CHAOS_MIN_EVENT_INTERVAL:-5}"
CHAOS_MAX_EVENT_INTERVAL="${CHAOS_MAX_EVENT_INTERVAL:-15}"
CHAOS_MIN_CHECKPOINTS_BEFORE_DELETE="${CHAOS_MIN_CHECKPOINTS_BEFORE_DELETE:-2}"
CHAOS_MAX_NODE_DELETES="${CHAOS_MAX_NODE_DELETES:-2}"
CHAOS_NAMESPACE="${CHAOS_NAMESPACE:-chaos-test}"

# Diverse sample workloads
SAMPLE_WORKLOADS=(
    "counting.yaml"
    "counting-multicontainer.yaml"
    "monte-carlo-pi.yaml"
    "numpy-matrix-ops.yaml"
    "sklearn-random-forest.yaml"
    "xgboost-training.yaml"
)

# State tracking (file-based for subshell compatibility)
STATE_DIR="/tmp/chaos-state-$$"

setup_file() {
    mkdir -p "$STATE_DIR"
    echo "0" > "$STATE_DIR/checkpoint_count"
    echo "0" > "$STATE_DIR/restore_count"
    echo "0" > "$STATE_DIR/node_delete_count"

    create_namespace "$CHAOS_NAMESPACE"

    local node_count
    node_count=$(get_worker_node_count)
    info_log "Initial worker node count: $node_count"

    if [ "$node_count" -lt 2 ]; then
        skip "Insufficient nodes for chaos test: have $node_count, need at least 2"
    fi
}

teardown_file() {
    info_log "Cleaning up chaos test resources..."
    delete_namespace "$CHAOS_NAMESPACE" --force --timeout=120s 2>/dev/null || true

    # Cleanup checkpoints
    if [ -d "$STATE_DIR" ]; then
        for action_file in "$STATE_DIR"/action_*; do
            [ -f "$action_file" ] || continue
            local action_id
            action_id=$(cat "$action_file")
            local checkpoint_id
            checkpoint_id=$(get_checkpoint_id_from_action "$action_id" 2>/dev/null) || true
            if [ -n "$checkpoint_id" ]; then
                cleanup_checkpoint "$checkpoint_id" 2>/dev/null || true
            fi
        done
        rm -rf "$STATE_DIR"
    fi
}

#########################
# Node Helpers          #
#########################

get_worker_node_count() {
    kubectl get nodes --no-headers 2>/dev/null | grep -cv "control-plane\|master" || echo 0
}

get_worker_nodes() {
    kubectl get nodes --no-headers -o custom-columns=':metadata.name' 2>/dev/null | \
        grep -v "control-plane\|master" || true
}

delete_node() {
    local node="$1"
    info_log "EVENT: Deleting node $node"
    kubectl delete node "$node" --wait=true --timeout=60s 2>/dev/null || true
}

#########################
# State Management      #
#########################

# Pod states: running, checkpointing, checkpointed, restoring, restored, deleted
set_pod_state() {
    local pod="$1"
    local state="$2"
    echo "$state" > "$STATE_DIR/pod_${pod}"
}

get_pod_state() {
    local pod="$1"
    cat "$STATE_DIR/pod_${pod}" 2>/dev/null || echo "unknown"
}

set_pod_action_id() {
    local pod="$1"
    local action_id="$2"
    echo "$action_id" > "$STATE_DIR/action_${pod}"
}

get_pod_action_id() {
    local pod="$1"
    cat "$STATE_DIR/action_${pod}" 2>/dev/null
}

list_pods_in_state() {
    local target_state="$1"
    for f in "$STATE_DIR"/pod_*; do
        [ -f "$f" ] || continue
        local pod="${f##*/pod_}"
        local state
        state=$(cat "$f")
        if [ "$state" = "$target_state" ]; then
            echo "$pod"
        fi
    done
}

count_pods_in_state() {
    local target_state="$1"
    list_pods_in_state "$target_state" | wc -l
}

increment_counter() {
    local counter="$1"
    local val
    val=$(cat "$STATE_DIR/$counter")
    echo $((val + 1)) > "$STATE_DIR/$counter"
}

get_counter() {
    local counter="$1"
    cat "$STATE_DIR/$counter" 2>/dev/null || echo 0
}

add_deleted_node() {
    local node="$1"
    echo "$node" >> "$STATE_DIR/deleted_nodes"
}

get_deleted_node_count() {
    wc -l < "$STATE_DIR/deleted_nodes" 2>/dev/null || echo 0
}

#########################
# Chaos Event Handlers  #
#########################

do_checkpoint_random_pod() {
    local running_pods
    running_pods=$(list_pods_in_state "running")
    local count
    count=$(echo "$running_pods" | grep -c . 2>/dev/null || echo 0)

    if [ "$count" -eq 0 ]; then
        debug_log "No running pods to checkpoint"
        return 0
    fi

    # Pick random pod
    local pod
    pod=$(echo "$running_pods" | shuf -n 1)

    info_log "EVENT: Checkpointing pod $pod"
    set_pod_state "$pod" "checkpointing"

    local pod_id
    pod_id=$(get_pod_id "$pod" "$CHAOS_NAMESPACE" 2>/dev/null)
    if [ -z "$pod_id" ]; then
        error_log "Failed to get pod ID for $pod"
        set_pod_state "$pod" "running"
        return 1
    fi

    local action_id
    action_id=$(checkpoint_pod "$pod_id" 2>/dev/null)
    if [ $? -ne 0 ] || ! validate_action_id "$action_id" 2>/dev/null; then
        error_log "Checkpoint failed for $pod"
        set_pod_state "$pod" "running"
        return 1
    fi

    set_pod_action_id "$pod" "$action_id"

    # Poll for completion (with timeout)
    if poll_action_status "$action_id" "checkpoint" 120 2>/dev/null; then
        info_log "Checkpoint complete for $pod (action: $action_id)"
        set_pod_state "$pod" "checkpointed"
        increment_counter "checkpoint_count"
    else
        error_log "Checkpoint timed out for $pod"
        set_pod_state "$pod" "running"
        return 1
    fi
}

do_delete_random_node() {
    local deleted_count
    deleted_count=$(get_deleted_node_count)

    if [ "$deleted_count" -ge "$CHAOS_MAX_NODE_DELETES" ]; then
        debug_log "Max node deletes reached ($deleted_count)"
        return 0
    fi

    local nodes
    nodes=$(get_worker_nodes)
    local node_count
    node_count=$(echo "$nodes" | grep -c . 2>/dev/null || echo 0)

    if [ "$node_count" -le 1 ]; then
        debug_log "Only $node_count node(s) remaining, skipping delete"
        return 0
    fi

    # Pick random node
    local node
    node=$(echo "$nodes" | shuf -n 1)

    delete_node "$node"
    add_deleted_node "$node"
    increment_counter "node_delete_count"

    info_log "Node $node deleted (total deleted: $((deleted_count + 1)))"
}

do_restore_random_checkpoint() {
    local checkpointed_pods
    checkpointed_pods=$(list_pods_in_state "checkpointed")
    local count
    count=$(echo "$checkpointed_pods" | grep -c . 2>/dev/null || echo 0)

    if [ "$count" -eq 0 ]; then
        debug_log "No checkpointed pods to restore"
        return 0
    fi

    # Pick random checkpointed pod
    local pod
    pod=$(echo "$checkpointed_pods" | shuf -n 1)
    local action_id
    action_id=$(get_pod_action_id "$pod")

    if [ -z "$action_id" ]; then
        error_log "No action ID for pod $pod"
        return 1
    fi

    info_log "EVENT: Restoring pod $pod from checkpoint $action_id"
    set_pod_state "$pod" "restoring"

    # Delete original pod first
    kubectl delete pod "$pod" -n "$CHAOS_NAMESPACE" --wait=true --timeout=30s 2>/dev/null || true

    # Trigger restore
    local restore_id
    restore_id=$(restore_pod "$action_id" "$CLUSTER_ID" 2>/dev/null)
    if [ $? -ne 0 ]; then
        error_log "Restore failed for $pod"
        set_pod_state "$pod" "deleted"
        return 1
    fi

    info_log "Restore initiated for $pod (restore action: $restore_id)"
    set_pod_state "$pod" "restored"
    increment_counter "restore_count"
}

#########################
# Event Selection       #
#########################

select_random_event() {
    local running
    running=$(count_pods_in_state "running")
    local checkpointed
    checkpointed=$(count_pods_in_state "checkpointed")
    local checkpoint_count
    checkpoint_count=$(get_counter "checkpoint_count")
    local node_delete_count
    node_delete_count=$(get_deleted_node_count)
    local node_count
    node_count=$(get_worker_node_count)

    local options=()

    # CHECKPOINT: if we have running pods
    if [ "$running" -gt 0 ]; then
        options+=(CHECKPOINT CHECKPOINT CHECKPOINT)  # 3x weight
    fi

    # DELETE_NODE: if we have enough checkpoints and nodes to spare
    if [ "$checkpoint_count" -ge "$CHAOS_MIN_CHECKPOINTS_BEFORE_DELETE" ] && \
       [ "$node_delete_count" -lt "$CHAOS_MAX_NODE_DELETES" ] && \
       [ "$node_count" -gt 1 ]; then
        options+=(DELETE_NODE)
    fi

    # RESTORE: if we have checkpointed pods
    if [ "$checkpointed" -gt 0 ]; then
        options+=(RESTORE RESTORE)  # 2x weight
    fi

    if [ ${#options[@]} -eq 0 ]; then
        echo "NONE"
        return
    fi

    # Pick random from weighted options
    echo "${options[$((RANDOM % ${#options[@]}))]}"
}

#########################
# Main Chaos Loop       #
#########################

run_chaos_event_loop() {
    local start_time
    start_time=$(date +%s)
    local end_time=$((start_time + CHAOS_DURATION))
    local event_num=0

    info_log "Starting chaos event loop for ${CHAOS_DURATION}s"

    while [ "$(date +%s)" -lt "$end_time" ]; do
        # Random delay between events
        local delay=$((CHAOS_MIN_EVENT_INTERVAL + RANDOM % (CHAOS_MAX_EVENT_INTERVAL - CHAOS_MIN_EVENT_INTERVAL + 1)))
        sleep "$delay"

        ((event_num++))
        local elapsed=$(($(date +%s) - start_time))
        local event
        event=$(select_random_event)

        debug_log "Event #$event_num at ${elapsed}s: $event"

        case "$event" in
            CHECKPOINT)
                do_checkpoint_random_pod
                ;;
            DELETE_NODE)
                do_delete_random_node
                ;;
            RESTORE)
                do_restore_random_checkpoint
                ;;
            NONE)
                debug_log "No valid events available, waiting..."
                ;;
        esac
    done

    info_log "Chaos event loop complete after $event_num events"
}

#########################
# Main Chaos Test       #
#########################

# bats test_tags=chaos,stress,interleaved
@test "Chaos: Interleaved checkpoint/node-delete/restore events" {
    local workload_count=${#SAMPLE_WORKLOADS[@]}

    info_log "=========================================="
    info_log "Starting Interleaved Chaos Test"
    info_log "  Workloads: $workload_count"
    info_log "  Duration: ${CHAOS_DURATION}s"
    info_log "  Event interval: ${CHAOS_MIN_EVENT_INTERVAL}-${CHAOS_MAX_EVENT_INTERVAL}s"
    info_log "  Max node deletes: $CHAOS_MAX_NODE_DELETES"
    info_log "=========================================="

    # Phase 1: Deploy all workloads
    info_log "Phase 1: Deploying $workload_count diverse workloads..."

    for sample in "${SAMPLE_WORKLOADS[@]}"; do
        local spec
        spec=$(pod_spec "$SAMPLES_DIR/cpu/$sample" "$CHAOS_NAMESPACE")
        local pod_name
        pod_name=$(get_created_pod "$spec" "$CHAOS_NAMESPACE" 0)
        kubectl apply -f "$spec"
        set_pod_state "$pod_name" "deploying"
        debug_log "Deployed: $pod_name from $sample"
    done

    # Wait for all pods to be ready
    info_log "Waiting for all workloads to become Ready..."
    for f in "$STATE_DIR"/pod_*; do
        [ -f "$f" ] || continue
        local pod="${f##*/pod_}"
        if validate_pod "$pod" 180 "$CHAOS_NAMESPACE"; then
            set_pod_state "$pod" "running"
            info_log "Pod $pod is Ready"
        else
            error_log "Pod $pod failed to become Ready"
            set_pod_state "$pod" "failed"
        fi
    done

    local running_count
    running_count=$(count_pods_in_state "running")
    info_log "All $running_count workloads are Ready"

    # Phase 2: Run chaos event loop
    # TODO: Replace individual checkpoint calls with a namespace-level policy.
    # In production, configure a Cedana checkpoint policy for the entire
    # namespace that triggers checkpoints based on signals (spot interruption,
    # node pressure, schedule, etc.) rather than random events.
    info_log "Phase 2: Running chaos event loop..."
    run_chaos_event_loop

    # Phase 3: Final restoration of any remaining checkpointed pods
    info_log "Phase 3: Final cleanup - restoring remaining checkpointed pods..."

    local remaining_checkpointed
    remaining_checkpointed=$(list_pods_in_state "checkpointed")
    for pod in $remaining_checkpointed; do
        do_restore_random_checkpoint
    done

    # Wait for restored pods to come up
    sleep 15

    # Phase 4: Validate results
    info_log "Phase 4: Validating results..."

    local restored_pods
    restored_pods=$(list_restored_pods "$CHAOS_NAMESPACE")
    local success_count=0

    for restored_pod in $restored_pods; do
        if validate_pod "$restored_pod" 180 "$CHAOS_NAMESPACE"; then
            ((success_count++))
            info_log "Restored pod $restored_pod is Ready"
        else
            error_log "Restored pod $restored_pod failed"
        fi
    done

    local checkpoint_count
    checkpoint_count=$(get_counter "checkpoint_count")
    local restore_count
    restore_count=$(get_counter "restore_count")
    local node_delete_count
    node_delete_count=$(get_deleted_node_count)

    info_log "=========================================="
    info_log "Chaos Test Results"
    info_log "  Workloads deployed: $workload_count"
    info_log "  Checkpoints performed: $checkpoint_count"
    info_log "  Restores triggered: $restore_count"
    info_log "  Nodes deleted: $node_delete_count"
    info_log "  Successfully restored: $success_count"
    info_log "=========================================="

    # Success if at least 80% restored
    local min_success=$((workload_count * 80 / 100))
    if [ "$min_success" -lt 1 ]; then
        min_success=1
    fi

    [ "$success_count" -ge "$min_success" ] || {
        error_log "Too few successful restores: $success_count < $min_success"
        return 1
    }

    info_log "Chaos test passed!"
}

# bats test_tags=chaos,stress,rapid
@test "Chaos: Rapid checkpoint/restore cycles" {
    local cycles=3

    info_log "Testing $cycles rapid checkpoint/restore cycles..."

    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cpu/counting.yaml" "$CHAOS_NAMESPACE")
    local pod_name
    pod_name=$(grep -E "^\s*name:" "$spec" | head -1 | awk '{print $2}' | tr -d '"')

    kubectl apply -f "$spec"
    validate_pod "$pod_name" 120 "$CHAOS_NAMESPACE" || return 1

    local current_name="$pod_name"

    for i in $(seq 1 "$cycles"); do
        info_log "Cycle $i/$cycles: Checkpoint $current_name"

        # TODO: Replace with namespace-level policy checkpoint trigger
        local pod_id action_id
        pod_id=$(get_pod_id "$current_name" "$CHAOS_NAMESPACE")
        action_id=$(checkpoint_pod "$pod_id")
        [ $? -eq 0 ] || { error_log "Checkpoint failed"; return 1; }

        poll_action_status "$action_id" "checkpoint" 60 || return 1
        sleep 2

        kubectl delete pod "$current_name" -n "$CHAOS_NAMESPACE" --wait=true

        info_log "Cycle $i/$cycles: Restore"
        restore_pod "$action_id" "$CLUSTER_ID" || return 1

        sleep 5
        current_name=$(wait_for_cmd 60 get_restored_pod "$pod_name" "$CHAOS_NAMESPACE")
        [ -n "$current_name" ] || { error_log "No restored pod"; return 1; }

        validate_pod "$current_name" 120 "$CHAOS_NAMESPACE" || return 1
        pod_name="$current_name"
    done

    kubectl delete pod "$current_name" -n "$CHAOS_NAMESPACE" --wait=false 2>/dev/null || true
    info_log "Rapid cycles test passed"
}

# bats test_tags=chaos,stress,concurrent
@test "Chaos: Concurrent checkpoints" {
    info_log "Testing concurrent checkpoints..."

    local samples=("counting.yaml" "monte-carlo-pi.yaml" "numpy-matrix-ops.yaml" "counting-multicontainer.yaml")
    local pods=()

    for sample in "${samples[@]}"; do
        local spec
        spec=$(pod_spec "$SAMPLES_DIR/cpu/$sample" "$CHAOS_NAMESPACE")
        local name
        name=$(get_created_pod "$spec" "$CHAOS_NAMESPACE" 0)
        kubectl apply -f "$spec"
        pods+=("$name")
    done

    for pod_name in "${pods[@]}"; do
        validate_pod "$pod_name" 120 "$CHAOS_NAMESPACE" || return 1
    done

    sleep 10

    # TODO: Replace with namespace-level policy checkpoint trigger.
    # In production, triggering a checkpoint policy for the namespace would
    # handle all pods automatically rather than this manual concurrent approach.
    local pids=() action_files=()
    for pod_name in "${pods[@]}"; do
        local action_file="/tmp/chaos-action-$pod_name"
        action_files+=("$action_file")
        (
            local pid
            pid=$(get_pod_id "$pod_name" "$CHAOS_NAMESPACE")
            checkpoint_pod "$pid" > "$action_file" 2>&1
        ) &
        pids+=($!)
    done

    local failures=0
    for pid in "${pids[@]}"; do
        wait "$pid" || ((failures++))
    done

    for action_file in "${action_files[@]}"; do
        local action_id
        action_id=$(cat "$action_file")
        if validate_action_id "$action_id" 2>/dev/null; then
            poll_action_status "$action_id" "checkpoint" 120 || ((failures++))
        else
            ((failures++))
        fi
        rm -f "$action_file"
    done

    kubectl delete pods -n "$CHAOS_NAMESPACE" --all --wait=false 2>/dev/null || true

    [ "$failures" -eq 0 ] || { error_log "$failures checkpoints failed"; return 1; }
    info_log "Concurrent checkpoints test passed"
}
