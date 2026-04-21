#!/usr/bin/env bash

# Normalize the URL
if ! declare -f normalize_url &>/dev/null; then
    normalize_url() {
        local url="$1"
        url="${url%/}"
        url="${url%/v1}"
        if [[ ! "$url" =~ ^https?:// ]]; then
            url="https://$url"
        fi
        echo "$url"
    }
fi

PROPAGATOR_BASE_URL=""
if [ -n "${CEDANA_URL:-}" ]; then
    PROPAGATOR_BASE_URL=$(normalize_url "$CEDANA_URL")
fi
PROPAGATOR_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}"

validate_action_id() {
    local id="$1"
    id="${id//\"/}"

    if [[ "$id" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]; then
        debug_log "Valid action ID: $id"
    else
        error_log "Error: Invalid action ID: $id"
        return 1
    fi

    return 0
}

##############################
# Slurm Cluster Registration
##############################

# Register a slurm cluster via propagator API
# @param $1: Cluster name
# Returns: Cluster ID
register_slurm_cluster() {
    local name="$1"
    if [ -z "$name" ]; then
        error_log "register_slurm_cluster requires cluster name"
        return 1
    fi

    debug_log "Registering slurm cluster '$name' with propagator..."

    local response
    if ! response=$(curl -sS -X POST "${PROPAGATOR_BASE_URL}/v2/cluster" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
            -d '{ "cluster_name": "'"${name}"'", "kind": "slurm" }' \
        -w "%{http_code}"); then
        error_log "Failed to register slurm cluster: request error"
        return 1
    fi

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        local cluster_id="${body//\"/}"

        if command -v jq &>/dev/null; then
            local parsed_id
            parsed_id=$(jq -r 'if type == "string" then . else (.id // .cluster_id // .data.id // empty) end' <<<"$body" 2>/dev/null || true)
            if [ -n "$parsed_id" ] && [ "$parsed_id" != "null" ]; then
                cluster_id="$parsed_id"
            fi
        fi

        if [ -z "$cluster_id" ]; then
            error_log "Failed to parse cluster ID from register response: $body"
            return 1
        fi

        echo "$cluster_id"
        return 0
    else
        error_log "Failed to register slurm cluster (HTTP $http_code): $body"
        return 1
    fi
}

# Deregister a slurm cluster via propagator API
# @param $1: Cluster ID
deregister_slurm_cluster() {
    local id="$1"
    if [ -z "$id" ]; then
        error_log "deregister_slurm_cluster requires cluster ID"
        return 1
    fi

    debug_log "Deregistering slurm cluster '$id'..."

    local response
    if ! response=$(curl -sS -X DELETE "${PROPAGATOR_BASE_URL}/v2/cluster/${id}" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
        -w "%{http_code}"); then
        debug_log "Warning: Failed to deregister slurm cluster '$id': request error"
        return 0
    fi

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        debug_log "Slurm cluster deregistered"
        return 0
    else
        debug_log "Warning: Failed to deregister slurm cluster (HTTP $http_code): $body"
        return 0
    fi
}

#############################
# Slurm Job Checkpoint/Restore
#############################

# Checkpoint a slurm job via propagator API
# @param $1: Job ID (slurm numeric ID)
# @param $2: Kind (simple|rootfs|rootfsonly, default: simple)
# @param $3: Reason (heartbeat|manual, default: manual)
# Returns: Action ID (UUID)
checkpoint_slurm_job() {
    local job_id="$1"
    local kind="${2:-simple}"
    local reason="${3:-manual}"

    if [ -z "$job_id" ]; then
        error_log "checkpoint_slurm_job requires job_id"
        return 1
    fi

    debug_log "Checkpointing slurm job '$job_id' (kind=$kind, reason=$reason)..."

    local payload
    if ! payload=$(jq -n \
            --arg job_id "$job_id" \
            --arg job_name "job_$job_id" \
            --arg kind "$kind" \
            --arg reason "$reason" \
            '{"job_id": $job_id, "job_name": $job_name, "kind": $kind, "reason": $reason}'); then
        error_log "Failed to build checkpoint request payload"
        return 1
    fi

    info_log "Checkpoint request payload: $payload"

    local response
    if ! response=$(curl -sS -X POST "${PROPAGATOR_BASE_URL}/v2/slurm/checkpoint/job" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
            -d "$payload" \
        -w "%{http_code}"); then
        error_log "Failed to checkpoint slurm job: request error"
        return 1
    fi

    local http_code="${response: -3}"
    local body="${response%???}"

    info_log "Checkpoint response (HTTP $http_code): $body"

    if [ "$http_code" -eq 200 ]; then
        echo "$body"
        return 0
    else
        error_log "Failed to checkpoint slurm job (HTTP $http_code): $body"
        return 1
    fi
}

# Restore a slurm job via propagator API
# @param $1: Action ID (from checkpoint operation)
# @param $2: Cluster ID
# @param $3: Reason (nodeTermination|nodeUnschedulable|manual, default: manual)
# Returns: Action ID (UUID)
restore_slurm_job() {
    local action_id="$1"
    action_id="${action_id//\"/}"
    local cluster_id="$2"
    cluster_id="${cluster_id//\"/}"
    local reason="${3:-manual}"

    if [ -z "$action_id" ]; then
        error_log "restore_slurm_job requires action_id"
        return 1
    fi
    if [ -z "$cluster_id" ]; then
        error_log "restore_slurm_job requires cluster_id"
        return 1
    fi

    debug_log "Restoring slurm job from action '$action_id' in cluster '$cluster_id'..."

    local payload
    if ! payload=$(jq -n \
            --arg action_id "$action_id" \
            --arg cluster_id "$cluster_id" \
            --arg reason "$reason" \
            '{
                "action_id": $action_id,
                "cluster_id": $cluster_id,
                "reason": $reason
            }'); then
        error_log "Failed to build restore request payload"
        return 1
    fi

    local response
    if ! response=$(curl -sS -X POST "${PROPAGATOR_BASE_URL}/v2/slurm/restore/job" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
            -d "$payload" \
        -w "%{http_code}"); then
        error_log "Failed to restore slurm job: request error"
        return 1
    fi

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        echo "$body"
        return 0
    else
        error_log "Failed to restore slurm job (HTTP $http_code): $body"
        return 1
    fi
}

##############################
# Slurm Checkpoint Lifecycle
##############################

# Poll a slurm checkpoint/restore action until completion
# @param $1: Action ID
# @param $2: Operation type (checkpoint|restore) for logging
# @param $3: Timeout in seconds (default: 300)
poll_slurm_action_status() {
    local action_id="$1"
    action_id="${action_id//\"/}"
    local operation="${2:-operation}"
    local timeout_seconds=${3:-300}
    local interval=5
    local max_attempts=$((timeout_seconds / interval))
    local status=""

    if [ -z "$action_id" ]; then
        error_log "poll_slurm_action_status requires action_id"
        return 1
    fi

    debug_log "Polling status for slurm $operation action '$action_id'..."

    for i in $(seq 1 $max_attempts); do
        local response
        if ! response=$(curl -sS -X GET "${PROPAGATOR_BASE_URL}/v2/slurm/checkpoints" \
                -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
            -w "%{http_code}"); then
            debug_log "Warning: /v2/slurm/checkpoints request failed (attempt $i/$max_attempts)"
            sleep "$interval"
            continue
        fi

        local http_code="${response: -3}"
        local body="${response%???}"

        if [ "$http_code" -eq 200 ]; then

            local entry_status=""
            if ! entry_status=$(jq -r --arg aid "$action_id" \
                '.[] | select(.action_id == $aid) | .status' 2>/dev/null <<<"$body" | head -1); then
                entry_status=""
            fi

            if [ -n "$entry_status" ]; then
                status="$entry_status"
                debug_log "Slurm $operation action status: $status (attempt $i/$max_attempts)"

                case "$status" in
                    "ready")
                        debug_log "Slurm $operation action completed successfully"
                        return 0
                        ;;
                    "possibly_uploaded"|"deprecated"|"error"|"failed")
                        error_log "Slurm $operation action failed (checkpoint status: $status)"
                        return 1
                        ;;
                    *)
                        debug_log "Checkpoint status '$status', continuing..."
                        ;;
                esac
            else
                status="processing"
                debug_log "Slurm $operation action not yet in checkpoints list (attempt $i/$max_attempts)"
            fi
        else
            debug_log "Warning: /v2/slurm/checkpoints failed (HTTP $http_code), body: $body (attempt $i/$max_attempts)"
        fi

        sleep $interval
    done

    error_log "Timeout waiting for slurm $operation action '$action_id' (last status: $status)"
    return 1
}

# Get slurm checkpoints from propagator
# @param $1: Optional comma-separated checkpoint IDs
get_slurm_checkpoints() {
    local ids="${1:-}"

    local url="${PROPAGATOR_BASE_URL}/v2/slurm/checkpoints"
    if [ -n "$ids" ]; then
        url="${url}?ids=${ids}"
    fi

    local response
    if ! response=$(curl -sS -X GET "$url" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
        -w "%{http_code}"); then
        error_log "Failed to get slurm checkpoints: request error"
        return 1
    fi

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        echo "$body"
        return 0
    else
        error_log "Failed to get slurm checkpoints (HTTP $http_code): $body"
        return 1
    fi
}

# Deprecate a slurm checkpoint
# @param $1: Checkpoint ID
deprecate_slurm_checkpoint() {
    local checkpoint_id="$1"
    checkpoint_id="${checkpoint_id//\"/}"

    if [ -z "$checkpoint_id" ]; then
        error_log "deprecate_slurm_checkpoint requires checkpoint_id"
        return 1
    fi

    debug_log "Deprecating slurm checkpoint '$checkpoint_id'..."

    local response
    if ! response=$(curl -sS -X PATCH "${PROPAGATOR_BASE_URL}/v2/slurm/checkpoints/deprecate/${checkpoint_id}" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
        -w "%{http_code}"); then
        debug_log "Warning: Failed to deprecate checkpoint '$checkpoint_id': request error"
        return 0
    fi

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        debug_log "Slurm checkpoint deprecated"
        return 0
    else
        debug_log "Warning: Failed to deprecate checkpoint (HTTP $http_code): $body"
        return 0
    fi
}

##############################
# Slurm Job Sync
##############################

# Sync slurm jobs to propagator
# @param $1: Cluster ID
# @param $2: Jobs JSON array
sync_slurm_jobs() {
    local cluster_id="$1"
    local jobs_json="$2"

    if [ -z "$cluster_id" ] || [ -z "$jobs_json" ]; then
        error_log "sync_slurm_jobs requires cluster_id and jobs_json"
        return 1
    fi

    debug_log "Syncing slurm jobs to propagator..."

    local payload
    if ! payload=$(jq -n \
            --arg cluster_id "$cluster_id" \
            --argjson jobs "$jobs_json" \
            '{
                "cluster_id": $cluster_id,
                "jobs": $jobs
            }'); then
        error_log "Failed to build slurm job sync payload"
        return 1
    fi

    local response
    if ! response=$(curl -sS -X POST "${PROPAGATOR_BASE_URL}/v2/slurm/jobs/sync" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
            -d "$payload" \
        -w "%{http_code}"); then
        error_log "Failed to sync jobs: request error"
        return 1
    fi

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        debug_log "Jobs synced: $body"
        return 0
    else
        error_log "Failed to sync jobs (HTTP $http_code): $body"
        return 1
    fi
}

##############################
# Propagator Connectivity
##############################

# Validate propagator connectivity for slurm tests
validate_slurm_propagator() {
    debug_log "Validating propagator connectivity for slurm..."

    local response
    if ! response=$(curl -sS -X GET "${PROPAGATOR_BASE_URL}/v2/user" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
        -w "%{http_code}"); then
        error_log "Propagator connectivity validation failed: request error"
        return 1
    fi

    local http_code="${response: -3}"

    if [ "$http_code" -eq 200 ]; then
        debug_log "Propagator connectivity validated"
        return 0
    else
        error_log "Failed to connect to propagator (HTTP $http_code)"
        return 1
    fi
}
