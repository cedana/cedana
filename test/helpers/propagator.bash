#!/usr/bin/env bash

#####################################
### Cedana Propagator API Helpers ###
#####################################
#
# All functions talk to the propagator at CEDANA_URL (full base URL, no trailing
# slash) using CEDANA_AUTH_TOKEN. The caller is responsible for setting both.

# Checkpoint a pod via propagator API
# @param $1: Pod ID (UID)
# @param $2: Runc root path
# Returns: Action ID for polling
checkpoint_pod() {
    local pod_id="$1"
    local runc_root=${2:-"/run/containerd/runc/k8s.io"}
    local directory="${3:-}"   # overrides the configured checkpoint directory
    local compression="${4:-}" # overrides the configured compression

    if [ -z "$pod_id" ] || [ -z "$runc_root" ]; then
        error_log "checkpoint_pod requires pod_id and runc_root"
        return 1
    fi

    debug_log "Checkpointing pod '$pod_id' with runc root '$runc_root'..."

    local payload
    payload=$(jq -n \
            --arg pod_id "$pod_id" \
            --arg runc_root "$runc_root" \
            --arg directory "$directory" \
            --arg compression "$compression" \
            '{
            "pod_id": $pod_id,
            "runc_root": $runc_root
    } + ({"directory": $directory, "compression": $compression} | with_entries(select(.value != "")) |
        if . == {} then {} else {"overrides": .} end)')

    local response
    response=$(curl -s -X POST "${CEDANA_URL}/checkpoint/pod" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
            -d "$payload" \
        -w "%{http_code}")

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        echo "$body"
        return 0
    else
        error_log "Failed to checkpoint pod (HTTP $http_code): $body"
        return 1
    fi
}

checkpoint_pod_by_name() {
    local name="$1"
    local namespace="$2"

    if [ -z "$name" ] || [ -z "$namespace" ]; then
        error_log "checkpoint_pod_by_name requires pod name and namespace"
        return 1
    fi

    debug_log "Checkpointing pod '$namespace/$name' by name..."

    local payload
    payload=$(jq -n --arg name "$name" --arg namespace "$namespace" \
        '{"pod_name": $name, "namespace": $namespace}')

    local response
    response=$(curl -s -X POST "${CEDANA_URL}/checkpoint/pod" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
            -d "$payload" \
        -w "%{http_code}")

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        echo "$body"
        return 0
    else
        error_log "Failed to checkpoint pod by name (HTTP $http_code): $body"
        return 1
    fi
}

# Restore a pod via propagator API
# @param $1: Action ID from checkpoint operation
# @param $2: Cluster ID (optional, uses same cluster if not provided)
# Returns: Action ID for polling
restore_pod() {
    local action_id="$1"
    action_id="${action_id//\"/}" # remove quotes if present
    local cluster_id="$2"
    cluster_id="${cluster_id//\"/}" # remove quotes if present

    if [ -z "$cluster_id" ]; then
        error_log "restore_pod requires cluster_id"
        return 1
    fi

    if [ -z "$action_id" ]; then
        error_log "restore_pod requires action_id"
        return 1
    fi

    debug_log "Restoring pod from action '$action_id' in cluster '$cluster_id'..."

    local payload
    if [ -n "$cluster_id" ]; then
        payload=$(jq -n \
                --arg action_id "$action_id" \
                --arg cluster_id "$cluster_id" \
                '{
                "action_id": $action_id,
                "cluster_id": $cluster_id,
                "reason": "manual"
        }')
    else
        payload=$(jq -n \
                --arg action_id "$action_id" \
                '{
                "action_id": $action_id
        }')
    fi

    local response
    response=$(curl -s -X POST "${CEDANA_URL}/restore/pod" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
            -d "$payload" \
        -w "%{http_code}" 2>&1)

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        echo "$body"
        return 0
    else
        error_log "Error: Failed to restore pod (HTTP $http_code): $body"
        return 1
    fi
}

# Poll action status until completion using the dedicated status endpoint
# @param $1: Action ID
# @param $2: Operation type (checkpoint|restore) for logging
poll_action_status() {
    local action_id="$1"
    action_id="${action_id//\"/}" # remove quotes if present
    local operation="${2:-operation}"
    local timeout_seconds=${3:-300}

    local interval=5
    local timeout=$((timeout_seconds / interval))

    if [ -z "$action_id" ]; then
        error_log "poll_action_status requires action_id"
        return 1
    fi

    debug_log "Polling status for $operation action '$action_id'..."

    for i in $(seq 1 $timeout); do
        local response
        response=$(curl -s -X GET "${CEDANA_URL}/checkpoint/status/${action_id}" \
                -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
            -w "%{http_code}")

        local http_code="${response: -3}"
        local body="${response%???}"

        if [ "$http_code" -eq 200 ]; then
            local status
            status=$(echo "$body" | jq -r '.status' 2>/dev/null)

            debug_log "Action status: $status (attempt $i/60)"

            case "$status" in
                "ready")
                    debug_log "$operation action completed successfully"
                    return 0
                    ;;
                "error")
                    local details
                    details=$(echo "$body" | jq -r '.details // "No details available"' 2>/dev/null)
                    error_log "$operation action failed with status '$status'"
                    debug_log "Error details: $details"
                    return 1
                    ;;
                "initialized"|"processing"|"checkpoint_created")
                    # Continue polling for these statuses
                    ;;
                *)
                    debug_log "Warning: Unknown status '$status', continuing to poll..."
                    ;;
            esac
        elif [ "$http_code" -eq 404 ]; then
            debug_log "Warning: Dedicated status endpoint not found, trying general actions endpoint..."
            # Fallback to general actions endpoint
            local actions_response
            actions_response=$(curl -s -X GET "${CEDANA_URL}/actions" \
                    -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
                -w "%{http_code}")

            local actions_http_code="${actions_response: -3}"
            local actions_body="${actions_response%???}"

            if [ "$actions_http_code" -eq 200 ]; then
                local action_info
                action_info=$(echo "$actions_body" | jq --arg id "$action_id" '.[] | select(.action_id == $id)' 2>/dev/null)

                if [ -n "$action_info" ]; then
                    local status
                    status=$(echo "$action_info" | jq -r '.status' 2>/dev/null)

                    debug_log "Action status (from actions API): $status (attempt $i/60)"

                    case "$status" in
                        "success"|"completed"|"ready")
                            debug_log "$operation action completed successfully"
                            return 0
                            ;;
                        "failed"|"error")
                            error_log "$operation action failed with status '$status'"
                            debug_log "Action details: $action_info"
                            return 1
                            ;;
                        *)
                            # Continue polling for other statuses
                            ;;
                    esac
                else
                    debug_log "Warning: Action '$action_id' not found in actions list (attempt $i/60)"
                fi
            else
                debug_log "Warning: Failed to get actions list (HTTP $actions_http_code) (attempt $i/60)"
            fi
        else
            debug_log "Warning: Failed to get action status (HTTP $http_code): $body (attempt $i/60)"
        fi

        sleep $interval
    done

    error_log "Timeout waiting for $operation action '$action_id' to complete (last status: $status)"
    return 1
}

# Polls a restore until it's done, and fails unless it actually restored. A pod that becomes
# Ready proves nothing: a restore that falls back to a cold start (status "fallback"), or that
# never runs (e.g. the runtime takes the run path), still ends with a Ready pod.
# @param $1: Restore action ID (same as the restore's UUID)
# @param $2: Timeout in seconds (optional, defaults to 300)
poll_restore_status() {
    local restore_id="$1"
    restore_id="${restore_id//\"/}" # remove quotes if present
    local timeout_seconds=${2:-300}
    local interval=5

    if [ -z "$restore_id" ]; then
        error_log "poll_restore_status requires a restore action ID"
        return 1
    fi

    debug_log "Polling restore $restore_id..."

    local status="" restore=""
    for _ in $(seq 1 $((timeout_seconds / interval))); do
        local response
        response=$(curl -s --max-time 10 -X GET "${CEDANA_URL}/restores?restore_uuid=${restore_id}&limit=1" \
                -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
            -w "%{http_code}")
        local http_code="${response: -3}"
        local body="${response%???}"

        if [ "$http_code" -eq 200 ]; then
            restore=$(echo "$body" | jq -c '.[0] // empty' 2>/dev/null)
            status=$(echo "$restore" | jq -r '.status // empty' 2>/dev/null)
            case "$status" in
                "success")
                    debug_log "Restore $restore_id succeeded"
                    return 0
                    ;;
                "fallback"|"error"|"failed") # fallback: a cold start instead
                    error_log "Restore $restore_id ended as $status: $(echo "$restore" | jq -r '.error_message // "no error message"')"
                    debug_log "Restore record: $restore"
                    return 1
                    ;;
            esac
            debug_log "Restore status: ${status:-not recorded yet}"
        else
            debug_log "Failed to get restore $restore_id (HTTP $http_code): $body"
        fi
        sleep $interval
    done

    error_log "Restore $restore_id did not finish within ${timeout_seconds}s (last status: ${status:-not recorded}), e.g. it never ran"
    debug_log "Restore record: ${restore:-none}"
    return 1
}

# The status of a checkpoint (e.g. initialized, processing, ready, error), or nothing
# @param $1: Checkpoint action ID
get_checkpoint_status() {
    local action_id="${1//\"/}"
    curl -s --max-time 10 -X GET "${CEDANA_URL}/checkpoint/status/${action_id}" \
        -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" | jq -r '.status // empty' 2>/dev/null
}

# The status of a restore (e.g. starting, pod_created, success, fallback, error), or nothing
# if it isn't recorded yet
# @param $1: Restore action ID
get_restore_status() {
    local restore_id="${1//\"/}"
    curl -s --max-time 10 -X GET "${CEDANA_URL}/restores?restore_uuid=${restore_id}&limit=1" \
        -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" | jq -r '.[0].status // empty' 2>/dev/null
}

# Get checkpoint ID from action ID using the actions API
# @param $1: Action ID
# Returns: Checkpoint ID
get_checkpoint_id_from_action() {
    local action_id="$1"
    action_id="${action_id//\"/}" # remove quotes if present

    if [ -z "$action_id" ]; then
        error_log "get_checkpoint_id_from_action requires action_id"
        return 1
    fi

    debug_log "Getting checkpoint ID for action '$action_id'..."

    local response
    response=$(curl -s -X GET "${CEDANA_URL}/actions" \
            -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
        -w "%{http_code}")

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        local checkpoint_id
        checkpoint_id=$(echo "$body" | jq -r --arg id "$action_id" '.[] | select(.action_id == $id) | .checkpoint_id' 2>/dev/null)

        if [ -n "$checkpoint_id" ] && [ "$checkpoint_id" != "null" ]; then
            echo "$checkpoint_id"
            return 0
        else
            error_log "Could not find checkpoint_id for action '$action_id'"
            return 1
        fi
    else
        error_log "Failed to get actions (HTTP $http_code): $body"
        return 1
    fi
}

# Cleanup/deprecate a checkpoint
# @param $1: Checkpoint ID (can be extracted from action response)
cleanup_checkpoint() {
    local checkpoint_id="$1"
    checkpoint_id="${checkpoint_id//\"/}" # remove quotes if present

    if [ -z "$checkpoint_id" ]; then
        error_log "cleanup_checkpoint requires checkpoint_id"
        return 1
    fi

    debug_log "Deprecating checkpoint '$checkpoint_id'..."

    local response
    response=$(curl -s -X PATCH "${CEDANA_URL}/checkpoints/deprecate/${checkpoint_id}" \
            -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
        -w "%{http_code}")

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        debug_log "Checkpoint deprecated successfully"
        return 0
    else
        debug_log "Warning: Failed to deprecate checkpoint (HTTP $http_code): $body"
        # Don't fail the test if cleanup fails
        return 0
    fi
}

# Extract checkpoint ID from action response
# @param $1: Action response JSON
extract_checkpoint_id() {
    local action_response="$1"

    if [ -z "$action_response" ]; then
        error_log "extract_checkpoint_id requires action_response"
        return 1
    fi

    echo "$action_response" | jq -r '.checkpoint_id' 2>/dev/null || echo ""
}

# Validate propagator service connectivity
validate_propagator_connectivity() {
    debug_log "Validating propagator service connectivity..."

    local response
    response=$(curl -s -X GET "${CEDANA_URL}/user" \
            -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
        -w "%{http_code}")

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        debug_log "Propagator service connectivity validated"
        return 0
    else
        error_log "Failed to connect to propagator service (HTTP $http_code): $body"
        return 1
    fi
}

# Parse JSON response safely
# @param $1: JSON string
# @param $2: jq filter
parse_json_response() {
    local json="$1"
    local filter="$2"

    if [ -z "$json" ] || [ -z "$filter" ]; then
        error_log "parse_json_response requires json and filter"
        return 1
    fi

    echo "$json" | jq -r "$filter" 2>/dev/null || echo ""
}

# Get list of checkpoints from the propagator service
# @param $1: Optional cluster ID to filter by cluster
# Returns: JSON array of checkpoints
get_checkpoints() {
    local cluster_id="$1"
    cluster_id="${cluster_id//\"/}" # remove quotes if present

    debug_log "Retrieving checkpoints from propagator..."

    local url="${CEDANA_URL}/checkpoints"
    if [ -n "$cluster_id" ]; then
        url="${url}?cluster_id=${cluster_id}"
    fi

    local response
    response=$(curl -s -X GET "$url" \
            -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
        -w "%{http_code}")

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        echo "$body"
        return 0
    else
        error_log "Failed to get checkpoints (HTTP $http_code): $body"
        return 1
    fi
}

# List checkpoints in a human-readable format
# @param $1: Optional cluster ID to filter by cluster
list_checkpoints() {
    local cluster_id="$1"
    cluster_id="${cluster_id//\"/}" # remove quotes if present

    local checkpoints
    checkpoints=$(get_checkpoints "$cluster_id")
    local exit_code=$?

    if [ $exit_code -ne 0 ]; then
        error_log "Failed to retrieve checkpoints"
        return $exit_code
    fi

    if [ -z "$checkpoints" ] || [ "$checkpoints" = "[]" ] || [ "$checkpoints" = "null" ]; then
        debug_log "No checkpoints found"
        return 0
    fi

    echo "=== Cedana Checkpoints ==="

    # Parse and display checkpoints in readable format
    echo "$checkpoints" | jq -r '.[] | "ID: \(.id // "N/A")\nStatus: \(.status // "N/A")\nPod: \(.pod_name // "N/A")\nNamespace: \(.namespace // "N/A")\nCluster: \(.cluster_id // "N/A")\nCreated: \(.created_at // "N/A")\n---"' 2>/dev/null || {
        echo "Raw response:"
        echo "$checkpoints"
    }
}

# Get latest action_id belonging to a given pod_id using the dedicated endpoint
# @param $1: Pod ID
# Returns: Action ID (plain text)
get_latest_pod_action_id() {
    local pod_id="$1"
    pod_id="${pod_id//\"/}" # remove quotes if present

    if [ -z "$pod_id" ]; then
        error_log "get_latest_pod_action_id requires pod_id"
        return 1
    fi

    debug_log "Getting latest action ID for pod '$pod_id'..."

    local response
    response=$(curl -s -X GET "${CEDANA_URL}/actions/from_pod/${pod_id}" \
            -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
        -w "%{http_code}")

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        # Response is plain text (action_id)
        echo "$body"
        return 0
    elif [ "$http_code" -eq 404 ]; then
        error_log "Error: No action found for pod '$pod_id'"
        return 1
    else
        error_log "Error: Failed to get action for pod (HTTP $http_code): $body"
        return 1
    fi
}

# Register a new cluster via the propagator API
register_cluster() {
    local name="$1"
    if [ -z "$name" ]; then
        error_log "Error: register_cluster requires cluster name"
        return 1
    fi

    debug_log "Registering a new cluster with name '$name'..."

    local response
    response=$(curl -s -X POST "${CEDANA_URL}/cluster" \
            -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
            -H "Content-Type: application/json" \
            -d '{ "cluster_name": "'"${name}"'" }' \
        -w "%{http_code}")

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        echo "$body"
        return 0
    else
        error_log "Failed to register clusters (HTTP $http_code): $body"
        return 1
    fi
}

# Deregister an existing cluster via the propagator API
deregister_cluster() {
    local id="$1"
    if [ -z "$id" ]; then
        error_log "Error: deregister_cluster requires cluster ID"
        return 1
    fi

    debug_log "Deregistering a new cluster with ID '$id'..."

    local response
    response=$(curl -s -X DELETE "${CEDANA_URL}/cluster/${id}" \
            -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" \
        -w "%{http_code}")

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        echo "$body"
        return 0
    else
        error_log "Failed to register clusters (HTTP $http_code): $body"
        return 1
    fi
}

validate_action_id() {
    local id="$1"
    id="${id//\"/}" # remove quotes if present

    if [[ "$id" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]; then
        debug_log "✅ Valid action ID: $id"
    else
        error_log "Error: Invalid action ID: $id"
        return 1
    fi

    return 0
}
