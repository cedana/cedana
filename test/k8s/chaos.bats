#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,chaos,stress,cpu

# Chaos/Stress Test for Cedana Checkpoint/Migrate/Restore
#
# This test exercises checkpoint/migrate/restore under realistic chaos conditions:
#
# - Deploys multiple diverse workloads across nodes
# - Checkpoints fire asynchronously (fire-and-forget)
# - Restores trigger automatically when:
#   1. A checkpoint completes (immediate restore to validate)
#   2. A node goes down (restore workloads that were on that node)
# - Node deletions simulate failures
#
# Environment variables:
#   CHAOS_DURATION                        - Total chaos duration in seconds (default: 120)
#   CHAOS_MIN_EVENT_INTERVAL              - Min seconds between events (default: 3)
#   CHAOS_MAX_EVENT_INTERVAL              - Max seconds between events (default: 8)
#   CHAOS_MIN_CHECKPOINTS_BEFORE_DELETE   - Min checkpoints before allowing node delete (default: 2)
#   CHAOS_MAX_NODE_DELETES                - Max nodes to delete (default: 2)
#   CHAOS_NAMESPACE                       - Namespace for chaos test pods (default: chaos-test)

load ../helpers/utils
load ../helpers/daemon
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

################################################################################
# Configuration
################################################################################

CHAOS_DURATION="${CHAOS_DURATION:-120}"
CHAOS_MIN_EVENT_INTERVAL="${CHAOS_MIN_EVENT_INTERVAL:-3}"
CHAOS_MAX_EVENT_INTERVAL="${CHAOS_MAX_EVENT_INTERVAL:-8}"
CHAOS_MAX_NODE_DELETES="${CHAOS_MAX_NODE_DELETES:-2}"
CHAOS_CHECKPOINTS_PER_NODE_DELETE="${CHAOS_CHECKPOINTS_PER_NODE_DELETE:-3}"  # Force node delete every N checkpoints
CHAOS_NAMESPACE="${CHAOS_NAMESPACE:-chaos-test}"

# Sample workloads from cedana-samples (heavier workloads for realistic testing)
SAMPLE_WORKLOADS=(
    "monte-carlo-pi.yaml"
    "numpy-matrix-ops.yaml"
    "sklearn-random-forest.yaml"
    "xgboost-training.yaml"
)

# State directory for file-based state tracking (subshell compatible)
# Use fixed path since bats runs setup_file and tests in different processes
STATE_DIR="/tmp/chaos-state-bats"

################################################################################
# Setup / Teardown
################################################################################

setup_file() {
    rm -rf "$STATE_DIR"
    mkdir -p "$STATE_DIR"
    touch "$STATE_DIR/deleted_nodes"

    create_namespace "$CHAOS_NAMESPACE"

    local all_nodes deletable_nodes manager_node
    all_nodes=$(get_all_nodes | wc -l)
    deletable_nodes=$(get_deletable_node_count)
    manager_node=$(get_cedana_manager_node)

    info_log "Nodes: $all_nodes total, $deletable_nodes deletable (manager on: $manager_node)"

    [ "$deletable_nodes" -ge 1 ] || skip "Need at least 1 deletable node for chaos test"
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
            action_id="${action_id//\"/}"
            local checkpoint_id
            checkpoint_id=$(get_checkpoint_id_from_action "$action_id" 2>/dev/null) || true
            [ -n "$checkpoint_id" ] && cleanup_checkpoint "$checkpoint_id" 2>/dev/null || true
        done
        rm -rf "$STATE_DIR"
    fi
}

################################################################################
# Node Helpers
################################################################################

get_all_nodes() {
    kubectl get nodes --no-headers -o custom-columns=':metadata.name' 2>/dev/null || true
}

get_cedana_manager_node() {
    kubectl get pods -n "$CEDANA_NAMESPACE" -l app.kubernetes.io/component=manager \
        -o jsonpath='{.items[0].spec.nodeName}' 2>/dev/null || true
}

# Get nodes eligible for deletion (all nodes except the one running cedana-manager)
get_deletable_nodes() {
    local all_nodes manager_node
    all_nodes=$(get_all_nodes)
    manager_node=$(get_cedana_manager_node)

    if [ -n "$manager_node" ]; then
        echo "$all_nodes" | grep -vF "$manager_node"
    else
        echo "$all_nodes"
    fi
}

get_deletable_node_count() {
    local count
    count=$(get_deletable_nodes | grep -c . 2>/dev/null) || count=0
    echo "$count"
}

get_pod_node() {
    local pod="$1"
    kubectl get pod "$pod" -n "$CHAOS_NAMESPACE" -o jsonpath='{.spec.nodeName}' 2>/dev/null || true
}

################################################################################
# State Management (file-based for subshell compatibility)
################################################################################

# Pod states: running, checkpointing, checkpointed, restoring, restored, failed
set_pod_state() {
    local pod="$1" state="$2"
    echo "$state" > "$STATE_DIR/state_${pod}"
}

get_pod_state() {
    local pod="$1"
    cat "$STATE_DIR/state_${pod}" 2>/dev/null || echo "unknown"
}

set_pod_action_id() {
    local pod="$1" action_id="$2"
    echo "$action_id" > "$STATE_DIR/action_${pod}"
}

get_pod_action_id() {
    local pod="$1"
    local id
    id=$(cat "$STATE_DIR/action_${pod}" 2>/dev/null)
    echo "${id//\"/}"  # Strip quotes
}

set_pod_node() {
    local pod="$1" node="$2"
    echo "$node" > "$STATE_DIR/node_${pod}"
}

get_stored_pod_node() {
    local pod="$1"
    cat "$STATE_DIR/node_${pod}" 2>/dev/null
}

list_pods_in_state() {
    local target_state="$1"
    for f in "$STATE_DIR"/state_*; do
        [ -f "$f" ] || continue
        local pod="${f##*/state_}"
        [ "$(cat "$f")" = "$target_state" ] && echo "$pod"
    done
}

count_pods_in_state() {
    local count
    count=$(list_pods_in_state "$1" | grep -c . 2>/dev/null) || count=0
    echo "$count"
}

list_all_tracked_pods() {
    for f in "$STATE_DIR"/state_*; do
        [ -f "$f" ] || continue
        echo "${f##*/state_}"
    done
}

add_deleted_node() {
    echo "$1" >> "$STATE_DIR/deleted_nodes"
}

get_deleted_node_count() {
    local count
    count=$(grep -c . "$STATE_DIR/deleted_nodes" 2>/dev/null) || count=0
    echo "$count"
}

is_node_deleted() {
    local node="$1"
    grep -qx "$node" "$STATE_DIR/deleted_nodes" 2>/dev/null
}

increment_counter() {
    local counter="$1"
    local val
    val=$(cat "$STATE_DIR/$counter" 2>/dev/null || echo 0)
    echo $((val + 1)) > "$STATE_DIR/$counter"
}

get_counter() {
    cat "$STATE_DIR/$1" 2>/dev/null || echo 0
}

################################################################################
# Checkpoint Status Check (non-blocking)
################################################################################

check_checkpoint_status() {
    local action_id="$1"
    action_id="${action_id//\"/}"

    local base_url
    base_url=$(normalize_url "$CEDANA_URL")
    local response
    response=$(curl -s -X GET "${base_url}/v2/checkpoint/status/${action_id}" \
        -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
        -w "%{http_code}" 2>/dev/null)

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        echo "$body" | jq -r '.status' 2>/dev/null
    else
        echo "unknown"
    fi
}

################################################################################
# Event Handlers
################################################################################

# Checkpoint a specific pod (async - fire and forget)
do_checkpoint_pod() {
    local pod="$1"

    local pod_id
    pod_id=$(get_pod_id "$pod" "$CHAOS_NAMESPACE" 2>/dev/null)
    if [ -z "$pod_id" ]; then
        error_log "Failed to get pod ID for $pod"
        return 1
    fi

    info_log "CHECKPOINT: $pod"
    set_pod_state "$pod" "checkpointing"

    local action_id
    action_id=$(checkpoint_pod "$pod_id" 2>/dev/null)
    if [ $? -ne 0 ] || ! validate_action_id "$action_id" 2>/dev/null; then
        error_log "Checkpoint API call failed for $pod"
        set_pod_state "$pod" "running"
        return 1
    fi

    set_pod_action_id "$pod" "$action_id"
    increment_counter "checkpoint_initiated"
    debug_log "Checkpoint initiated: $pod -> $action_id"
}

# Restore a pod from its checkpoint
do_restore_pod() {
    local pod="$1"

    local action_id
    action_id=$(get_pod_action_id "$pod")
    if [ -z "$action_id" ]; then
        error_log "No action ID for pod $pod"
        return 1
    fi

    info_log "RESTORE: $pod (from $action_id)"
    set_pod_state "$pod" "restoring"

    # Delete the original pod if it still exists
    kubectl delete pod "$pod" -n "$CHAOS_NAMESPACE" --wait=false --timeout=10s 2>/dev/null || true

    # Trigger restore via API
    local restore_id
    restore_id=$(restore_pod "$action_id" "$CLUSTER_ID" 2>/dev/null)
    if [ $? -ne 0 ]; then
        error_log "Restore API call failed for $pod"
        set_pod_state "$pod" "failed"
        return 1
    fi

    set_pod_state "$pod" "restored"
    increment_counter "restore_triggered"
    debug_log "Restore triggered: $pod -> $restore_id"
}

# Delete a random eligible node (any node except the one running cedana-manager)
do_delete_node() {
    local deleted_count
    deleted_count=$(get_deleted_node_count)

    if [ "$deleted_count" -ge "$CHAOS_MAX_NODE_DELETES" ]; then
        debug_log "Max node deletes reached ($deleted_count)"
        return 1
    fi

    local nodes
    nodes=$(get_deletable_nodes)
    local node_count
    node_count=$(echo "$nodes" | grep -c . 2>/dev/null || echo 0)

    if [ "$node_count" -eq 0 ]; then
        debug_log "No deletable nodes available"
        return 1
    fi

    # Pick random node from deletable nodes
    local node
    node=$(echo "$nodes" | shuf -n 1)

    info_log "DELETE NODE: $node"
    kubectl delete node "$node" --wait=false --timeout=60s 2>/dev/null || true
    add_deleted_node "$node"
    increment_counter "node_deleted"

    # Find pods that were on this node and have checkpoints - restore them
    restore_pods_from_deleted_node "$node"
}

# Restore all checkpointed pods that were on a deleted node
restore_pods_from_deleted_node() {
    local deleted_node="$1"

    for pod in $(list_all_tracked_pods); do
        local pod_node
        pod_node=$(get_stored_pod_node "$pod")
        local state
        state=$(get_pod_state "$pod")

        # If pod was on the deleted node and has a checkpoint, restore it
        if [ "$pod_node" = "$deleted_node" ] && [ "$state" = "checkpointed" ]; then
            info_log "Auto-restoring $pod (was on deleted node $deleted_node)"
            do_restore_pod "$pod"
        fi
    done
}

################################################################################
# Main Chaos Loop
################################################################################

run_chaos_loop() {
    local start_time end_time event_num=0
    start_time=$(date +%s)
    end_time=$((start_time + CHAOS_DURATION))

    info_log "Starting chaos loop for ${CHAOS_DURATION}s"

    while [ "$(date +%s)" -lt "$end_time" ]; do
        event_num=$((event_num + 1))
        local elapsed=$(($(date +%s) - start_time))

        # Check pending checkpoints and trigger restores when complete
        process_pending_checkpoints

        # Discover and track any new restored pods that are now running
        discover_restored_pods

        # Random delay
        local delay=$((CHAOS_MIN_EVENT_INTERVAL + RANDOM % (CHAOS_MAX_EVENT_INTERVAL - CHAOS_MIN_EVENT_INTERVAL + 1)))
        sleep "$delay"

        # Select and execute event
        local event
        event=$(select_event)
        debug_log "Event #$event_num at ${elapsed}s: $event"

        case "$event" in
            CHECKPOINT)
                local pod
                pod=$(list_pods_in_state "running" | shuf -n 1)
                [ -n "$pod" ] && do_checkpoint_pod "$pod" || true
                ;;
            DELETE_NODE)
                do_delete_node || true
                ;;
            NONE)
                debug_log "No events available"
                ;;
        esac
    done

    # Final processing of pending checkpoints
    info_log "Processing remaining checkpoints..."
    for _ in $(seq 1 12); do
        process_pending_checkpoints
        [ "$(count_pods_in_state "checkpointing")" -eq 0 ] && break
        sleep 5
    done

    info_log "Chaos loop complete ($event_num events)"
}

# Discover restored pods that are now running and track them
# Shows which node the restored pod landed on
discover_restored_pods() {
    local restored_pods
    restored_pods=$(kubectl get pods -n "$CHAOS_NAMESPACE" --no-headers -o custom-columns=':metadata.name' 2>/dev/null | grep "restored" || true)

    for pod in $restored_pods; do
        [ -z "$pod" ] && continue
        # Skip if already tracked
        [ -f "$STATE_DIR/state_${pod}" ] && continue

        # Check if pod is running
        local phase
        phase=$(kubectl get pod "$pod" -n "$CHAOS_NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
        if [ "$phase" = "Running" ]; then
            local node
            node=$(get_pod_node "$pod")
            set_pod_state "$pod" "running"
            set_pod_node "$pod" "$node"
            increment_counter "restore_completed"

            # Extract short node name for cleaner output
            local short_node="${node##computeinstance-}"
            info_log "RESTORED: $pod -> node: $short_node"
        fi
    done
}

# Process checkpointing pods - when checkpoint completes, trigger restore
process_pending_checkpoints() {
    for pod in $(list_pods_in_state "checkpointing"); do
        local action_id
        action_id=$(get_pod_action_id "$pod")
        [ -n "$action_id" ] || continue

        local status
        status=$(check_checkpoint_status "$action_id")

        case "$status" in
            "ready")
                info_log "Checkpoint complete: $pod"
                set_pod_state "$pod" "checkpointed"
                increment_counter "checkpoint_completed"
                # Immediately restore from this checkpoint
                do_restore_pod "$pod"
                ;;
            "error")
                error_log "Checkpoint failed: $pod"
                set_pod_state "$pod" "failed"
                ;;
        esac
    done
}

# Select next event based on current state
# Returns: CHECKPOINT, DELETE_NODE, or NONE
select_event() {
    local running_count checkpoint_count node_delete_count deletable_count
    running_count=$(count_pods_in_state "running")
    checkpoint_count=$(get_counter "checkpoint_completed")
    node_delete_count=$(get_deleted_node_count)
    deletable_count=$(get_deletable_node_count)

    local can_checkpoint=false
    local can_delete_node=false

    # Can checkpoint if we have running pods
    [ "$running_count" -gt 0 ] && can_checkpoint=true

    # Can delete node only if:
    # 1. We have at least CHAOS_CHECKPOINTS_PER_NODE_DELETE completed checkpoints
    # 2. Haven't hit max node deletes
    # 3. Have deletable nodes remaining
    if [ "$checkpoint_count" -ge "$CHAOS_CHECKPOINTS_PER_NODE_DELETE" ] && \
       [ "$node_delete_count" -lt "$CHAOS_MAX_NODE_DELETES" ] && \
       [ "$deletable_count" -gt 0 ]; then
        can_delete_node=true
    fi

    # Force node deletion after every N completed checkpoints
    # e.g., with CHAOS_CHECKPOINTS_PER_NODE_DELETE=3: delete after checkpoint 3, 6, 9...
    local expected_deletes=$((checkpoint_count / CHAOS_CHECKPOINTS_PER_NODE_DELETE))
    if [ "$checkpoint_count" -ge "$CHAOS_CHECKPOINTS_PER_NODE_DELETE" ]; then
        info_log "select: ckpt=$checkpoint_count deletable=$deletable_count can_del=$can_delete_node expected_del=$expected_deletes actual_del=$node_delete_count"
    fi

    if $can_delete_node && [ "$expected_deletes" -gt "$node_delete_count" ]; then
        info_log "FORCING DELETE_NODE (ckpt=$checkpoint_count, expected=$expected_deletes, actual=$node_delete_count)"
        echo "DELETE_NODE"
        return
    fi

    # Random selection from available options
    local options=()
    $can_checkpoint && options+=(CHECKPOINT CHECKPOINT CHECKPOINT)  # 75% weight
    $can_delete_node && options+=(DELETE_NODE)                       # 25% weight

    [ ${#options[@]} -eq 0 ] && echo "NONE" && return
    echo "${options[$((RANDOM % ${#options[@]}))]}"
}

################################################################################
# Main Test
################################################################################

# bats test_tags=chaos,stress,interleaved
@test "Chaos: Interleaved checkpoint/node-delete/restore" {
    local workload_count=${#SAMPLE_WORKLOADS[@]}

    info_log "=========================================="
    info_log "Chaos Test Configuration"
    info_log "  Workloads: $workload_count"
    info_log "  Duration: ${CHAOS_DURATION}s"
    info_log "  Event interval: ${CHAOS_MIN_EVENT_INTERVAL}-${CHAOS_MAX_EVENT_INTERVAL}s"
    info_log "  Node delete: every ${CHAOS_CHECKPOINTS_PER_NODE_DELETE} checkpoints (max ${CHAOS_MAX_NODE_DELETES})"
    info_log "=========================================="

    # Phase 1: Deploy workloads
    # Samples use generateName (e.g., monte-carlo-pi-) so pods get names like monte-carlo-pi-xyz123
    info_log "Phase 1: Deploying workloads..."
    for sample in "${SAMPLE_WORKLOADS[@]}"; do
        local base_name
        base_name="${sample%.yaml}"

        # Deploy with namespace override (samples have namespace: default hardcoded)
        sed '/^  namespace:/d' "$SAMPLES_DIR/cpu/$sample" | kubectl create -n "$CHAOS_NAMESPACE" -f -

        # Wait briefly for pod to be created, then find it by prefix
        sleep 1
        local actual_pod
        actual_pod=$(kubectl get pods -n "$CHAOS_NAMESPACE" --no-headers -o custom-columns=':metadata.name' 2>/dev/null | grep "^${base_name}" | tail -1)

        if [ -n "$actual_pod" ]; then
            set_pod_state "$actual_pod" "deploying"
            info_log "Deployed: $actual_pod"
        else
            error_log "Failed to find pod for $sample"
        fi
    done

    # Wait for pods and record their nodes
    info_log "Waiting for workloads to be Ready..."
    for pod in $(list_all_tracked_pods); do
        if validate_pod "$pod" 300 "$CHAOS_NAMESPACE"; then
            set_pod_state "$pod" "running"
            local node
            node=$(get_pod_node "$pod")
            set_pod_node "$pod" "$node"
            info_log "Ready: $pod (node: $node)"
        else
            set_pod_state "$pod" "failed"
            error_log "Failed to start: $pod"
        fi
    done

    local running_count
    running_count=$(count_pods_in_state "running")
    info_log "$running_count workloads running"

    # Phase 2: Chaos loop
    info_log "Phase 2: Running chaos loop..."
    run_chaos_loop

    # Results
    local checkpoints_completed=$(get_counter checkpoint_completed)
    local restores_completed=$(get_counter restore_completed)
    local nodes_deleted=$(get_deleted_node_count)

    info_log "=========================================="
    info_log "Results"
    info_log "  Workloads deployed: $workload_count"
    info_log "  Checkpoints completed: $checkpoints_completed"
    info_log "  Restores completed: $restores_completed"
    info_log "  Nodes deleted: $nodes_deleted"
    info_log "=========================================="

    # Pass if we completed at least 1 checkpoint and 1 restore
    [ "$checkpoints_completed" -ge 1 ] || {
        error_log "No checkpoints completed"
        return 1
    }
    [ "$restores_completed" -ge 1 ] || {
        error_log "No restores completed"
        return 1
    }

    info_log "Chaos test passed!"
}

# bats test_tags=chaos,stress,rapid
@test "Chaos: Rapid checkpoint/restore cycles" {
    local cycles=3
    info_log "Testing $cycles rapid checkpoint/restore cycles..."

    local spec pod_name
    spec=$(pod_spec "$SAMPLES_DIR/cpu/monte-carlo-pi.yaml" "$CHAOS_NAMESPACE")
    pod_name=$(get_created_pod "$spec" "$CHAOS_NAMESPACE" 0)

    kubectl apply -f "$spec"
    validate_pod "$pod_name" 180 "$CHAOS_NAMESPACE" || return 1

    local current_name="$pod_name"

    for i in $(seq 1 "$cycles"); do
        info_log "Cycle $i/$cycles"

        # Checkpoint
        local pod_id action_id
        pod_id=$(get_pod_id "$current_name" "$CHAOS_NAMESPACE")
        action_id=$(checkpoint_pod "$pod_id")
        [ $? -eq 0 ] || { error_log "Checkpoint failed"; return 1; }

        poll_action_status "$action_id" "checkpoint" 120 || return 1

        # Delete and restore
        kubectl delete pod "$current_name" -n "$CHAOS_NAMESPACE" --wait=true
        restore_pod "$action_id" "$CLUSTER_ID" || return 1

        # Wait for restored pod
        sleep 5
        current_name=$(wait_for_cmd 120 get_restored_pod "$pod_name" "$CHAOS_NAMESPACE")
        [ -n "$current_name" ] || { error_log "No restored pod found"; return 1; }

        validate_pod "$current_name" 180 "$CHAOS_NAMESPACE" || return 1
        pod_name="$current_name"
    done

    kubectl delete pod "$current_name" -n "$CHAOS_NAMESPACE" --wait=false 2>/dev/null || true
    info_log "Rapid cycles test passed"
}

# bats test_tags=chaos,stress,concurrent
@test "Chaos: Concurrent checkpoints" {
    info_log "Testing concurrent checkpoints..."

    local samples=("monte-carlo-pi.yaml" "numpy-matrix-ops.yaml" "sklearn-random-forest.yaml")
    local pods=()

    for sample in "${samples[@]}"; do
        local spec name
        spec=$(pod_spec "$SAMPLES_DIR/cpu/$sample" "$CHAOS_NAMESPACE")
        name=$(get_created_pod "$spec" "$CHAOS_NAMESPACE" 0)
        kubectl apply -f "$spec"
        pods+=("$name")
    done

    for pod in "${pods[@]}"; do
        validate_pod "$pod" 300 "$CHAOS_NAMESPACE" || return 1
    done

    sleep 10

    # Checkpoint all concurrently
    # TODO: Use namespace-level checkpoint policy in production
    local pids=() action_files=()
    for pod in "${pods[@]}"; do
        local action_file="/tmp/chaos-action-$pod"
        action_files+=("$action_file")
        (
            local pid
            pid=$(get_pod_id "$pod" "$CHAOS_NAMESPACE")
            checkpoint_pod "$pid" > "$action_file" 2>&1
        ) &
        pids+=($!)
    done

    local failures=0
    for pid in "${pids[@]}"; do
        wait "$pid" || failures=$((failures + 1))
    done

    for action_file in "${action_files[@]}"; do
        local action_id
        action_id=$(cat "$action_file")
        if validate_action_id "$action_id" 2>/dev/null; then
            poll_action_status "$action_id" "checkpoint" 180 || failures=$((failures + 1))
        else
            failures=$((failures + 1))
        fi
        rm -f "$action_file"
    done

    kubectl delete pods -n "$CHAOS_NAMESPACE" --all --wait=false 2>/dev/null || true

    [ "$failures" -eq 0 ] || { error_log "$failures checkpoints failed"; return 1; }
    info_log "Concurrent checkpoints test passed"
}
