#!/usr/bin/env bash

#####################################
### Cedana Propagator API Helpers ###
#####################################

# Default propagator service configuration
# Normalize the URL to ensure it includes protocol and has the correct format
normalize_url() {
    local url="$1"
    # Remove trailing slashes and /v1 suffix
    url="${url%/}"
    url="${url%/v1}"

    # Add https:// if no protocol specified
    if [[ ! "$url" =~ ^https?:// ]]; then
        url="https://$url"
    fi

    echo "$url"
}

PROPAGATOR_BASE_URL=""
if [ -n "${CEDANA_URL:-}" ]; then
    PROPAGATOR_BASE_URL=$(normalize_url "$CEDANA_URL")
fi

PROPAGATOR_AUTH_TOKEN="${CEDANA_AUTH_TOKEN}"

# Checkpoint a pod via propagator API
# @param $1: Pod ID (UID)
# @param $2: Runc root path
# Returns: Action ID for polling
checkpoint_pod() {
    local pod_id="$1"
    local runc_root=${2:-"/run/containerd/runc/k8s.io"}

    if [ -z "$pod_id" ] || [ -z "$runc_root" ]; then
        error_log "checkpoint_pod requires pod_id and runc_root"
        return 1
    fi

    debug_log "Checkpointing pod '$pod_id' with runc root '$runc_root'..."

    local payload
    payload=$(jq -n \
            --arg pod_id "$pod_id" \
            --arg runc_root "$runc_root" \
            '{
            "pod_id": $pod_id,
            "runc_root": $runc_root
    }')

    local response
    response=$(curl -s -X POST "${PROPAGATOR_BASE_URL}/v2/checkpoint/pod" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
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
    response=$(curl -s -X POST "${PROPAGATOR_BASE_URL}/v2/restore/pod" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
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

    if [ -z "$action_id" ]; then
        error_log "poll_action_status requires action_id"
        return 1
    fi

    debug_log "Polling status for $operation action '$action_id'..."

    for i in $(seq 1 60); do  # 5 minute timeout
        local response
        response=$(curl -s -X GET "${PROPAGATOR_BASE_URL}/v2/checkpoint/status/${action_id}" \
                -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
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
            actions_response=$(curl -s -X GET "${PROPAGATOR_BASE_URL}/v2/actions" \
                    -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
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

        sleep 5
    done

    error_log "Timeout waiting for $operation action '$action_id' to complete (last status: $status)"
    return 1
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
    response=$(curl -s -X GET "${PROPAGATOR_BASE_URL}/v2/actions" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
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
    response=$(curl -s -X PATCH "${PROPAGATOR_BASE_URL}/v2/checkpoints/deprecate/${checkpoint_id}" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
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
    response=$(curl -s -X GET "${PROPAGATOR_BASE_URL}/v2/user" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
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

    local url="${PROPAGATOR_BASE_URL}/v2/checkpoints"
    if [ -n "$cluster_id" ]; then
        url="${url}?cluster_id=${cluster_id}"
    fi

    local response
    response=$(curl -s -X GET "$url" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
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
    response=$(curl -s -X GET "${PROPAGATOR_BASE_URL}/v2/actions/from_pod/${pod_id}" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
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
    response=$(curl -s -X POST "${PROPAGATOR_BASE_URL}/v2/cluster" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
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
    response=$(curl -s -X DELETE "${PROPAGATOR_BASE_URL}/v2/cluster/${id}" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
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
