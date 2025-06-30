#!/usr/bin/env bash

########################################
### Cedana Propagator API Helpers   ###
########################################

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
else
    PROPAGATOR_BASE_URL="http://192.168.0.71:1324"
fi

PROPAGATOR_AUTH_TOKEN="${CEDANA_AUTH_TOKEN}"

#
# Set up authentication for propagator API calls
# @param $1: Auth token (optional, uses CEDANA_AUTH_TOKEN if not provided)
#
setup_propagator_auth() {
    local token="${1:-$CEDANA_AUTH_TOKEN}"

    if [ -z "$token" ]; then
        echo "Error: CEDANA_AUTH_TOKEN not set and no token provided"
        return 1
    fi

    export PROPAGATOR_AUTH_TOKEN="$token"
    echo "Propagator authentication configured"
    return 0
}

#
# Get available clusters from the propagator service
# Returns JSON array of clusters
#
get_available_clusters() {
    echo "Retrieving available clusters from propagator..."

    local response
    response=$(curl -s -X GET "${PROPAGATOR_BASE_URL}/v1/cluster" \
        -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
        -w "%{http_code}")

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        echo "$body"
        return 0
    else
        echo "Error: Failed to get clusters (HTTP $http_code): $body" >&2
        return 1
    fi
}

#
# Get cluster ID (uses first available cluster or returns placeholder)
#
get_cluster_id() {
    local clusters
    clusters=$(get_available_clusters 2>/dev/null)

    if [ $? -eq 0 ] && [ -n "$clusters" ] && [ "$clusters" != "[]" ] && [ "$clusters" != "null" ]; then
        local cluster_id
        cluster_id=$(echo "$clusters" | jq -r '.[0].id' 2>/dev/null)
        if [ -n "$cluster_id" ] && [ "$cluster_id" != "null" ]; then
            echo "$cluster_id"
            return 0
        fi
    fi

    # Return a placeholder cluster ID if none available
    echo "test-cluster"
    return 0
}

#
# Checkpoint a pod via propagator API
# @param $1: Pod name
# @param $2: Runc root path
# @param $3: Namespace
# @param $4: Cluster ID
# Returns: Action ID for polling
#
checkpoint_pod_via_api() {
    local pod_name="$1"
    local runc_root="$2"
    local namespace="$3"
    local cluster_id="$4"

    if [ -z "$pod_name" ] || [ -z "$runc_root" ] || [ -z "$namespace" ] || [ -z "$cluster_id" ]; then
        echo "Error: checkpoint_pod_via_api requires pod_name, runc_root, namespace, cluster_id" >&2
        return 1
    fi

    echo "Checkpointing pod '$pod_name' in namespace '$namespace'..." >&2

    local payload
    payload=$(jq -n \
        --arg pod_name "$pod_name" \
        --arg runc_root "$runc_root" \
        --arg namespace "$namespace" \
        --arg cluster_id "$cluster_id" \
        '{
            "pod_name": $pod_name,
            "runc_root": $runc_root,
            "namespace": $namespace,
            "cluster_id": $cluster_id
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
        echo "Error: Failed to checkpoint pod (HTTP $http_code): $body" >&2
        return 1
    fi
}

#
# Restore a pod via propagator API
# @param $1: Action ID from checkpoint operation
# @param $2: Cluster ID (optional, uses same cluster if not provided)
# Returns: Action ID for polling
#
restore_pod_via_api() {
    local action_id="$1"
    local cluster_id="$2"

    if [ -z "$action_id" ]; then
        echo "Error: restore_pod_via_api requires action_id" >&2
        return 1
    fi

    echo "Restoring pod from action '$action_id'..." >&2
    echo "DEBUG: Using cluster_id: '$cluster_id'" >&2
    echo "DEBUG: Propagator base URL: '$PROPAGATOR_BASE_URL'" >&2

    local payload
    if [ -n "$cluster_id" ]; then
        payload=$(jq -n \
            --arg action_id "$action_id" \
            --arg cluster_id "$cluster_id" \
            '{
                "action_id": $action_id,
                "cluster_id": $cluster_id
            }')
    else
        payload=$(jq -n \
            --arg action_id "$action_id" \
            '{
                "action_id": $action_id
            }')
    fi

    echo "DEBUG: Restore payload: $payload" >&2

    local response
    response=$(curl -s -X POST "${PROPAGATOR_BASE_URL}/v2/restore/pod" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
        -d "$payload" \
        -w "%{http_code}" 2>&1)

    local curl_exit_code=$?
    echo "DEBUG: Curl exit code: $curl_exit_code" >&2

    local http_code="${response: -3}"
    local body="${response%???}"

    echo "DEBUG: Restore response HTTP $http_code: $body" >&2

    if [ "$http_code" -eq 200 ]; then
        echo "$body"
        return 0
    else
        echo "Error: Failed to restore pod (HTTP $http_code): $body" >&2
        return 1
    fi
}

#
# Poll action status until completion using the dedicated status endpoint
# @param $1: Action ID
# @param $2: Operation type (checkpoint|restore) for logging
#
poll_action_status() {
    local action_id="$1"
    local operation="${2:-operation}"

    if [ -z "$action_id" ]; then
        echo "Error: poll_action_status requires action_id" >&2
        return 1
    fi

    echo "Polling status for $operation action '$action_id'..." >&2

    for i in $(seq 1 60); do  # 5 minute timeout
        local response
        response=$(curl -s -X GET "${PROPAGATOR_BASE_URL}/v2/checkpoint/status/${action_id}" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
            -w "%{http_code}")

        local http_code="${response: -3}"
        local body="${response%???}"

        echo "DEBUG: Status API response (attempt $i): HTTP $http_code, Body: $body" >&2

        if [ "$http_code" -eq 200 ]; then
            local status
            status=$(echo "$body" | jq -r '.status' 2>/dev/null)

            echo "Action status: $status (attempt $i/60)" >&2

            case "$status" in
                "ready")
                    echo "$operation action completed successfully" >&2
                    return 0
                    ;;
                "error")
                    local details
                    details=$(echo "$body" | jq -r '.details // "No details available"' 2>/dev/null)
                    echo "Error: $operation action failed with status '$status'" >&2
                    echo "Error details: $details" >&2
                    return 1
                    ;;
                "initialized"|"processing"|"checkpoint_created")
                    # Continue polling for these statuses
                    ;;
                *)
                    echo "Warning: Unknown status '$status', continuing to poll..." >&2
                    ;;
            esac
        elif [ "$http_code" -eq 404 ]; then
            echo "Warning: Dedicated status endpoint not found, trying general actions endpoint..." >&2
            # Fallback to general actions endpoint
            local actions_response
            actions_response=$(curl -s -X GET "${PROPAGATOR_BASE_URL}/v2/actions" \
                -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
                -w "%{http_code}")

            local actions_http_code="${actions_response: -3}"
            local actions_body="${actions_response%???}"

            echo "DEBUG: Actions API response (attempt $i): HTTP $actions_http_code" >&2

            if [ "$actions_http_code" -eq 200 ]; then
                local action_info
                action_info=$(echo "$actions_body" | jq --arg id "$action_id" '.[] | select(.action_id == $id)' 2>/dev/null)

                if [ -n "$action_info" ]; then
                    local status
                    status=$(echo "$action_info" | jq -r '.status' 2>/dev/null)

                    echo "Action status (from actions API): $status (attempt $i/60)" >&2

                    case "$status" in
                        "success"|"completed"|"ready")
                            echo "$operation action completed successfully" >&2
                            return 0
                            ;;
                        "failed"|"error")
                            echo "Error: $operation action failed with status '$status'" >&2
                            echo "Action details: $action_info" >&2
                            return 1
                            ;;
                        *)
                            # Continue polling for other statuses
                            ;;
                    esac
                else
                    echo "Warning: Action '$action_id' not found in actions list (attempt $i/60)" >&2
                fi
            else
                echo "Warning: Failed to get actions list (HTTP $actions_http_code) (attempt $i/60)" >&2
            fi
        else
            echo "Warning: Failed to get action status (HTTP $http_code): $body (attempt $i/60)" >&2
        fi

        sleep 5
    done

    echo "Error: Timeout waiting for $operation action '$action_id' to complete" >&2
    return 1
}

#
# Get checkpoint ID from action ID using the actions API
# @param $1: Action ID
# Returns: Checkpoint ID
#
get_checkpoint_id_from_action() {
    local action_id="$1"

    if [ -z "$action_id" ]; then
        echo "Error: get_checkpoint_id_from_action requires action_id" >&2
        return 1
    fi

    echo "Getting checkpoint ID for action '$action_id'..." >&2

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
            echo "Error: Could not find checkpoint_id for action '$action_id'" >&2
            return 1
        fi
    else
        echo "Error: Failed to get actions (HTTP $http_code): $body" >&2
        return 1
    fi
}

#
# Cleanup/deprecate a checkpoint
# @param $1: Checkpoint ID (can be extracted from action response)
#
cleanup_checkpoint() {
    local checkpoint_id="$1"

    if [ -z "$checkpoint_id" ]; then
        echo "Error: cleanup_checkpoint requires checkpoint_id" >&2
        return 1
    fi

    echo "Deprecating checkpoint '$checkpoint_id'..."

    local response
    response=$(curl -s -X PATCH "${PROPAGATOR_BASE_URL}/v2/checkpoints/deprecate/${checkpoint_id}" \
        -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
        -w "%{http_code}")

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        echo "Checkpoint deprecated successfully"
        return 0
    else
        echo "Warning: Failed to deprecate checkpoint (HTTP $http_code): $body" >&2
        # Don't fail the test if cleanup fails
        return 0
    fi
}

#
# Extract checkpoint ID from action response
# @param $1: Action response JSON
#
extract_checkpoint_id() {
    local action_response="$1"

    if [ -z "$action_response" ]; then
        echo "Error: extract_checkpoint_id requires action_response" >&2
        return 1
    fi

    echo "$action_response" | jq -r '.checkpoint_id' 2>/dev/null || echo ""
}

#
# Validate propagator service connectivity
#
validate_propagator_connectivity() {
    echo "Validating propagator service connectivity..."

    local response
    response=$(curl -s -X GET "${PROPAGATOR_BASE_URL}/v2/user" \
        -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
        -w "%{http_code}")

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        echo "Propagator service connectivity validated"
        return 0
    else
        echo "Error: Failed to connect to propagator service (HTTP $http_code): $body" >&2
        return 1
    fi
}

#
# Parse JSON response safely
# @param $1: JSON string
# @param $2: jq filter
#
parse_json_response() {
    local json="$1"
    local filter="$2"

    if [ -z "$json" ] || [ -z "$filter" ]; then
        echo "Error: parse_json_response requires json and filter" >&2
        return 1
    fi

    echo "$json" | jq -r "$filter" 2>/dev/null || echo ""
}

#
# Get list of checkpoints from the propagator service
# @param $1: Optional cluster ID to filter by cluster
# Returns: JSON array of checkpoints
#
get_checkpoints() {
    local cluster_id="$1"

    echo "Retrieving checkpoints from propagator..."

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
        echo "Error: Failed to get checkpoints (HTTP $http_code): $body" >&2
        return 1
    fi
}

#
# List checkpoints in a human-readable format
# @param $1: Optional cluster ID to filter by cluster
#
list_checkpoints() {
    local cluster_id="$1"

    echo "=== Cedana Checkpoints ==="

    local checkpoints
    checkpoints=$(get_checkpoints "$cluster_id")
    local exit_code=$?

    if [ $exit_code -ne 0 ]; then
        echo "Failed to retrieve checkpoints"
        return $exit_code
    fi

    if [ -z "$checkpoints" ] || [ "$checkpoints" = "[]" ] || [ "$checkpoints" = "null" ]; then
        echo "No checkpoints found"
        return 0
    fi

    # Parse and display checkpoints in readable format
    echo "$checkpoints" | jq -r '.[] | "ID: \(.id // "N/A")\nStatus: \(.status // "N/A")\nPod: \(.pod_name // "N/A")\nNamespace: \(.namespace // "N/A")\nCluster: \(.cluster_id // "N/A")\nCreated: \(.created_at // "N/A")\n---"' 2>/dev/null || {
        echo "Raw response:"
        echo "$checkpoints"
    }
}

#
# Get latest action_id belonging to a given pod_id using the dedicated endpoint
# @param $1: Pod ID
# Returns: Action ID (plain text)
#
get_latest_pod_action_id() {
    local pod_id="$1"

    if [ -z "$pod_id" ]; then
        echo "Error: get_latest_pod_action_id requires pod_id" >&2
        return 1
    fi

    echo "Getting latest action ID for pod '$pod_id'..." >&2

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
        echo "Error: No action found for pod '$pod_id'" >&2
        return 1
    else
        echo "Error: Failed to get action for pod (HTTP $http_code): $body" >&2
        return 1
    fi
}

#
# Poll restore action status until completion using the actions API
# @param $1: Action ID
# @param $2: Operation type (checkpoint|restore) for logging
#
poll_restore_action_status() {
    local action_id="$1"
    local operation="${2:-restore}"

    if [ -z "$action_id" ]; then
        echo "Error: poll_restore_action_status requires action_id" >&2
        return 1
    fi

    echo "Polling status for $operation action '$action_id'..."

    for i in $(seq 1 60); do  # 5 minute timeout
        local response
        response=$(curl -s -X GET "${PROPAGATOR_BASE_URL}/v2/actions" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
            -w "%{http_code}")

        local http_code="${response: -3}"
        local body="${response%???}"

        if [ "$http_code" -eq 200 ]; then
            local action_info
            action_info=$(echo "$body" | jq --arg id "$action_id" '.[] | select(.action_id == $id)' 2>/dev/null)

            if [ -n "$action_info" ]; then
                local status
                status=$(echo "$action_info" | jq -r '.status' 2>/dev/null)

                echo "Action status: $status (attempt $i/60)"

                case "$status" in
                    "success"|"completed"|"ready")
                        echo "$operation action completed successfully"
                        return 0
                        ;;
                    "failed"|"error")
                        echo "Error: $operation action failed with status '$status'" >&2
                        echo "Action details: $action_info" >&2
                        return 1
                        ;;
                    *)
                        # Continue polling for other statuses
                        ;;
                esac
            else
                echo "Warning: Action '$action_id' not found in response (attempt $i/60)"
            fi
        else
            echo "Warning: Failed to get actions (HTTP $http_code): $body (attempt $i/60)"
        fi

        sleep 5
    done

    echo "Error: Timeout waiting for $operation action '$action_id' to complete" >&2
    return 1
}
