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
    response=$(curl -s -X POST "${PROPAGATOR_BASE_URL}/v2/cluster" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
            -d '{ "cluster_name": "'"${name}"'" }' \
        -w "%{http_code}")

    local http_code="${response: -3}"
    local body="${response%???}"

    if [ "$http_code" -eq 200 ]; then
        echo "$body"
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
    response=$(curl -s -X DELETE "${PROPAGATOR_BASE_URL}/v2/cluster/${id}" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
        -w "%{http_code}")

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
# @param $1: Job ID (slurm job ID string or UUID)
# @param $2: Job name (optional)
# @param $3: Kind (simple|rootfs|rootfsonly, default: simple)
# @param $4: Reason (heartbeat|manual, default: manual)
# Returns: Action ID (UUID)
checkpoint_slurm_job() {
    local job_id="$1"
    local job_name="${2:-}"
    local kind="${3:-simple}"
    local reason="${4:-manual}"

    if [ -z "$job_id" ]; then
        error_log "checkpoint_slurm_job requires job_id"
        return 1
    fi

    debug_log "Checkpointing slurm job '$job_id' (kind=$kind, reason=$reason)..."

    local payload
    payload=$(jq -n \
            --arg job_id "$job_id" \
            --arg kind "$kind" \
            --arg reason "$reason" \
            '{
                "job_id": $job_id,
                "kind": $kind,
                "reason": $reason
            }')

    if [ -n "$job_name" ]; then
        payload=$(echo "$payload" | jq --arg job_name "$job_name" '. + {"job_name": $job_name}')
    fi

    local response
    response=$(curl -s -X POST "${PROPAGATOR_BASE_URL}/v2/slurm/checkpoint/job" \
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
    payload=$(jq -n \
            --arg action_id "$action_id" \
            --arg cluster_id "$cluster_id" \
            --arg reason "$reason" \
            '{
                "action_id": $action_id,
                "cluster_id": $cluster_id,
                "reason": $reason
            }')

    local response
    response=$(curl -s -X POST "${PROPAGATOR_BASE_URL}/v2/slurm/restore/job" \
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
        response=$(curl -s -X GET "${PROPAGATOR_BASE_URL}/v2/checkpoint/status/${action_id}" \
                -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
            -w "%{http_code}")

        local http_code="${response: -3}"
        local body="${response%???}"

        if [ "$http_code" -eq 200 ]; then
            status=$(echo "$body" | jq -r '.status' 2>/dev/null)
            debug_log "Action status: $status (attempt $i/$max_attempts)"

            case "$status" in
                "ready")
                    debug_log "Slurm $operation action completed successfully"
                    return 0
                    ;;
                "error")
                    local details
                    details=$(echo "$body" | jq -r '.details // "No details"' 2>/dev/null)
                    error_log "Slurm $operation action failed: $details"
                    return 1
                    ;;
                "initialized"|"processing"|"checkpoint_created")
                    ;;
                *)
                    debug_log "Warning: Unknown status '$status', continuing..."
                    ;;
            esac
        else
            debug_log "Warning: Status check failed (HTTP $http_code) (attempt $i/$max_attempts)"
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
    response=$(curl -s -X GET "$url" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
        -w "%{http_code}")

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
    response=$(curl -s -X PATCH "${PROPAGATOR_BASE_URL}/v2/slurm/checkpoints/deprecate/${checkpoint_id}" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
        -w "%{http_code}")

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
    payload=$(jq -n \
            --arg cluster_id "$cluster_id" \
            --argjson jobs "$jobs_json" \
            '{
                "cluster_id": $cluster_id,
                "jobs": $jobs
            }')

    local response
    response=$(curl -s -X POST "${PROPAGATOR_BASE_URL}/v2/slurm/jobs/sync" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
            -d "$payload" \
        -w "%{http_code}")

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
    response=$(curl -s -X GET "${PROPAGATOR_BASE_URL}/v2/user" \
            -H "Authorization: Bearer ${PROPAGATOR_AUTH_TOKEN}" \
        -w "%{http_code}")

    local http_code="${response: -3}"

    if [ "$http_code" -eq 200 ]; then
        debug_log "Propagator connectivity validated"
        return 0
    else
        error_log "Failed to connect to propagator (HTTP $http_code)"
        return 1
    fi
}
