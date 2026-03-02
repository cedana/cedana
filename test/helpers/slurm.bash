#!/usr/bin/env bash

########################
### Slurm Helpers    ###
########################

CEDANA_SLURM_DIR="${CEDANA_SLURM_DIR:-}"
SLURM_ANSIBLE_DIR="${CEDANA_SLURM_DIR}/ansible"

# Container names (matching docker-deploy.sh)
SLURM_CONTROLLER="slurm-controller"
SLURM_NODES=("slurm-compute-01" "slurm-compute-02")
SLURM_NETWORK="slurm-net"

##############################
# Ansible-based Cluster Mgmt
##############################

setup_slurm_cluster() {
    debug_log "Setting up SLURM cluster via ansible..."

    if [ -z "$CEDANA_SLURM_DIR" ]; then
        error_log "CEDANA_SLURM_DIR not set"
        return 1
    fi

    if [ ! -f "$SLURM_ANSIBLE_DIR/docker-deploy.sh" ]; then
        error_log "docker-deploy.sh not found at $SLURM_ANSIBLE_DIR/docker-deploy.sh"
        return 1
    fi

    # Install ansible if not available
    if ! command -v ansible-playbook &>/dev/null; then
        debug_log "Installing ansible..."
        pip3 install ansible sshpass 2>/dev/null || pip install ansible sshpass 2>/dev/null || {
            error_log "Failed to install ansible"
            return 1
        }
    fi

    debug_log "Running docker-deploy.sh from $SLURM_ANSIBLE_DIR..."
    pushd "$SLURM_ANSIBLE_DIR" > /dev/null
    ANSIBLE_EXTRA_ARGS="--skip-tags cedana" bash docker-deploy.sh
    local exit_code=$?
    popd > /dev/null

    if [ $exit_code -ne 0 ]; then
        error_log "Failed to deploy SLURM cluster"
        return 1
    fi

    # Wait for SLURM to be fully ready
    wait_for_slurm_ready 120
    debug_log "SLURM cluster is ready"
}

teardown_slurm_cluster() {
    debug_log "Tearing down SLURM cluster..."
    docker rm -f "$SLURM_CONTROLLER" "${SLURM_NODES[@]}" 2>/dev/null || true
    sleep 2
    docker network rm "$SLURM_NETWORK" 2>/dev/null || true
}

wait_for_slurm_ready() {
    local timeout=${1:-120}
    local elapsed=0

    debug_log "Waiting for SLURM cluster to be ready..."

    while [ $elapsed -lt $timeout ]; do
        if docker exec "$SLURM_CONTROLLER" sinfo -h 2>/dev/null | grep -q "idle\|mixed"; then
            debug_log "SLURM nodes are ready"
            docker exec "$SLURM_CONTROLLER" sinfo
            return 0
        fi
        sleep 3
        elapsed=$((elapsed + 3))
    done

    error_log "SLURM cluster not ready after ${timeout}s"
    docker exec "$SLURM_CONTROLLER" sinfo 2>/dev/null || true
    for node in "$SLURM_CONTROLLER" "${SLURM_NODES[@]}"; do
        debug_log "Logs from $node:"
        docker exec "$node" journalctl -u slurmctld -u slurmd --no-pager -n 50 2>/dev/null || true
    done
    return 1
}

##############################
# Cedana Installation in Cluster
##############################

# Install cedana binary, plugins, and slurm binaries into the SLURM cluster nodes.
install_cedana_in_slurm() {
    debug_log "Installing cedana into SLURM cluster..."

    local all_nodes=("$SLURM_CONTROLLER" "${SLURM_NODES[@]}")

    for node in "${all_nodes[@]}"; do
        debug_log "Installing cedana on $node..."

        # Copy cedana binary
        if command -v cedana &>/dev/null; then
            docker cp "$(which cedana)" "$node:/usr/local/bin/cedana"
            docker exec "$node" chmod +x /usr/local/bin/cedana
        fi

        # Copy all plugin libraries
        for lib in /usr/local/lib/libcedana-*.so; do
            if [ -f "$lib" ]; then
                docker cp "$lib" "$node:/usr/local/lib/$(basename "$lib")"
            fi
        done

        # Copy criu if available
        if command -v criu &>/dev/null; then
            docker cp "$(which criu)" "$node:/usr/local/bin/criu"
            docker exec "$node" chmod +x /usr/local/bin/criu
        fi

        # Copy slurm plugin binaries if available
        if [ -n "${CEDANA_SLURM_BIN:-}" ] && [ -f "$CEDANA_SLURM_BIN" ]; then
            docker cp "$CEDANA_SLURM_BIN" "$node:/usr/local/bin/cedana-slurm"
            docker exec "$node" chmod +x /usr/local/bin/cedana-slurm
        fi

        for so in libslurm-cedana.so task_cedana.so cli_filter_cedana.so; do
            if [ -f "/usr/local/lib/$so" ]; then
                docker cp "/usr/local/lib/$so" "$node:/usr/local/lib/$so"
            fi
        done

        # Install plugins inside the container
        docker exec "$node" cedana plugin install criu gpu slurm 2>/dev/null || true

        # Run slurm setup (configures plugstack.conf, copies .so to SLURM plugin dir, etc.)
        docker exec "$node" cedana slurm setup 2>/dev/null || true
    done

    # Restart SLURM services to pick up the new plugins
    docker exec "$SLURM_CONTROLLER" systemctl restart slurmctld 2>/dev/null || true
    for node in "${SLURM_NODES[@]}"; do
        docker exec "$node" systemctl restart slurmd 2>/dev/null || true
    done
    sleep 5

    debug_log "Cedana installed on all SLURM nodes"
}

# Install and start cedana-slurm daemon on a node
start_cedana_slurm_daemon() {
    local node="${1:-$SLURM_CONTROLLER}"

    debug_log "Starting cedana-slurm daemon on $node..."

    docker exec "$node" bash -c 'install -m 0600 -o root /dev/null /etc/cedana-slurm.env'

    printf 'CEDANA_URL=%s\nCEDANA_AUTH_TOKEN=%s\nRABBITMQ_URL=%s\nCEDANA_LOG_LEVEL=%s\n' \
        "${CEDANA_URL:-}" \
        "${CEDANA_AUTH_TOKEN:-}" \
        "${RABBITMQ_URL:-}" \
        "${CEDANA_LOG_LEVEL:-debug}" \
        | docker exec -i "$node" bash -c 'cat > /etc/cedana-slurm.env'

    docker exec "$node" bash -c 'cat > /etc/systemd/system/cedana-slurm.service' <<'UNIT'
[Unit]
Description=Cedana Slurm Daemon
After=network.target slurmctld.service

[Service]
Type=simple
EnvironmentFile=/etc/cedana-slurm.env
ExecStart=/usr/local/bin/cedana-slurm daemon start
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
UNIT

    docker exec "$node" systemctl daemon-reload
    docker exec "$node" systemctl start cedana-slurm

    sleep 3
    docker exec "$node" systemctl status cedana-slurm --no-pager || true
    debug_log "cedana-slurm daemon started on $node"
}

##############################
# Slurm Job Management
##############################

# Submit an sbatch job to the SLURM cluster
# @param $1: Path to sbatch file (local)
# Returns: SLURM job ID
slurm_submit_job() {
    local sbatch_file="$1"

    if [ ! -f "$sbatch_file" ]; then
        error_log "sbatch file not found: $sbatch_file"
        return 1
    fi

    local filename
    filename=$(basename "$sbatch_file")

    # Copy sbatch file to controller
    docker cp "$sbatch_file" "$SLURM_CONTROLLER:/data/$filename"

    # Submit job
    local output
    output=$(docker exec "$SLURM_CONTROLLER" sbatch --parsable "/data/$filename" 2>&1)
    local exit_code=$?

    if [ $exit_code -ne 0 ]; then
        error_log "Failed to submit sbatch job: $output"
        return 1
    fi

    local job_id
    job_id=$(echo "$output" | tail -1 | tr -d '[:space:]')

    debug_log "Submitted job $filename -> SLURM job ID: $job_id"
    echo "$job_id"
}

# Wait for a SLURM job to reach a specific state
# @param $1: SLURM job ID
# @param $2: Target state (RUNNING, COMPLETED, etc.)
# @param $3: Timeout in seconds (default: 60)
wait_for_slurm_job_state() {
    local job_id="$1"
    local target_state="$2"
    local timeout=${3:-60}
    local elapsed=0

    while [ $elapsed -lt $timeout ]; do
        local state
        state=$(docker exec "$SLURM_CONTROLLER" scontrol show job "$job_id" 2>/dev/null \
            | grep -oP 'JobState=\K\S+' || echo "UNKNOWN")

        debug_log "Job $job_id state: $state (want: $target_state)"

        if [ "$state" = "$target_state" ]; then
            return 0
        fi

        case "$state" in
            COMPLETED|FAILED|CANCELLED|TIMEOUT|NODE_FAIL)
                if [ "$state" != "$target_state" ]; then
                    error_log "Job $job_id reached terminal state $state (expected $target_state)"
                    return 1
                fi
                ;;
        esac

        sleep 2
        elapsed=$((elapsed + 2))
    done

    error_log "Timeout: job $job_id did not reach state $target_state (after ${timeout}s)"
    return 1
}

# Get the node a SLURM job is running on
# @param $1: SLURM job ID
get_slurm_job_node() {
    local job_id="$1"
    docker exec "$SLURM_CONTROLLER" scontrol show job "$job_id" 2>/dev/null \
        | grep -oP 'BatchHost=\K\S+' | head -1
}

# Get SLURM job info as JSON
# @param $1: SLURM job ID
get_slurm_job_info() {
    local job_id="$1"
    docker exec "$SLURM_CONTROLLER" scontrol show job "$job_id" -o 2>/dev/null
}

# Cancel a SLURM job
# @param $1: SLURM job ID
cancel_slurm_job() {
    local job_id="$1"
    docker exec "$SLURM_CONTROLLER" scancel "$job_id" 2>/dev/null || true
}

# Get the output of a SLURM job
# @param $1: SLURM job ID
# @param $2: Job name (for finding the output file)
get_slurm_job_output() {
    local job_id="$1"
    local job_name="${2:-}"

    local node
    node=$(get_slurm_job_node "$job_id")

    if [ -n "$job_name" ]; then
        docker exec "${node:-$SLURM_CONTROLLER}" cat "/data/${job_name}-${job_id}.out" 2>/dev/null || \
        docker exec "$SLURM_CONTROLLER" cat "/data/${job_name}-${job_id}.out" 2>/dev/null || true
    fi
}

##############################
# Slurm C/R Test Orchestrator
##############################

# @param $1: Action sequence (SUBMIT_DUMP, SUBMIT_DUMP_RESTORE, SUBMIT_DUMP_RESTORE_DUMP_RESTORE)
# @param $2: Path to sbatch file
# @param $3: Wait time after job starts before dumping (default: 10)
# @param $4: Dump timeout in seconds (default: 120)
test_slurm_job() {
    local action_sequence="$1"
    local sbatch_file="$2"
    local dump_wait_time="${3:-10}"
    local dump_timeout="${4:-120}"

    # Parse actions from sequence
    IFS='_' read -ra actions <<< "$action_sequence"

    local job_id=""
    local action_id=""
    local submitted=false
    local error=""

    for action in "${actions[@]}"; do
        case "$action" in
            SUBMIT)
                if [ "$submitted" = true ]; then
                    error="Cannot SUBMIT twice"
                    break
                fi

                debug_log "Submitting job from $sbatch_file..."
                job_id=$(slurm_submit_job "$sbatch_file")
                if [ -z "$job_id" ]; then
                    error="Failed to submit job"
                    break
                fi

                wait_for_slurm_job_state "$job_id" "RUNNING" 60 || {
                    error="Job $job_id failed to start running"
                    break
                }

                debug_log "Job $job_id is running, waiting ${dump_wait_time}s..."
                sleep "$dump_wait_time"
                submitted=true
                ;;

            DUMP)
                if [ "$submitted" = false ]; then
                    error="Cannot DUMP - no job submitted"
                    break
                fi
                if [ -z "$job_id" ]; then
                    error="Cannot DUMP - no active job"
                    break
                fi

                debug_log "Checkpointing slurm job $job_id via propagator..."
                local checkpoint_output
                checkpoint_output=$(checkpoint_slurm_job "$job_id")
                local checkpoint_status=$?

                if [ $checkpoint_status -ne 0 ]; then
                    error="Checkpoint failed: $checkpoint_output"
                    break
                fi

                action_id="$checkpoint_output"
                validate_action_id "$action_id" || {
                    error="Invalid action ID: $action_id"
                    break
                }

                poll_slurm_action_status "$action_id" "checkpoint" "$dump_timeout" || {
                    error="Checkpoint action $action_id did not complete"
                    break
                }

                debug_log "Checkpointed slurm job $job_id (action_id: $action_id)"
                ;;

            RESTORE)
                if [ -z "$action_id" ]; then
                    error="Cannot RESTORE - no checkpoint action ID"
                    break
                fi

                debug_log "Cancelling job $job_id before restore..."
                cancel_slurm_job "$job_id"
                sleep 2

                debug_log "Restoring slurm job from action $action_id..."
                local restore_output
                restore_output=$(restore_slurm_job "$action_id" "$SLURM_CLUSTER_ID")
                local restore_status=$?

                if [ $restore_status -ne 0 ]; then
                    error="Restore failed: $restore_output"
                    break
                fi

                local restore_action_id="$restore_output"
                validate_action_id "$restore_action_id" || {
                    error="Invalid restore action ID: $restore_action_id"
                    break
                }

                # Wait for a new job to appear (restore submits a new slurm job)
                debug_log "Waiting for restored job to start..."
                sleep 5

                # The restored job gets a new SLURM job ID
                # Find the most recent job from squeue
                local new_job_id
                new_job_id=$(docker exec "$SLURM_CONTROLLER" squeue -h -o "%i" --sort=-V 2>/dev/null | head -1)
                if [ -n "$new_job_id" ] && [ "$new_job_id" != "$job_id" ]; then
                    job_id="$new_job_id"
                    debug_log "Restored job has new ID: $job_id"
                fi

                wait_for_slurm_job_state "$job_id" "RUNNING" 60 || {
                    error="Restored job $job_id failed to start running"
                    break
                }

                debug_log "Restored job $job_id is running"
                submitted=true
                ;;

            *)
                error="Unknown action: $action"
                break
                ;;
        esac
    done

    # Cleanup
    if [ -n "$job_id" ]; then
        cancel_slurm_job "$job_id"
    fi

    if [ -n "$error" ]; then
        error_log "$error"
        # Print diagnostic info
        docker exec "$SLURM_CONTROLLER" squeue 2>/dev/null || true
        docker exec "$SLURM_CONTROLLER" sinfo 2>/dev/null || true
        return 1
    fi

    return 0
}

##############################
# Samples Directory Setup
##############################

setup_slurm_samples() {
    debug_log "Setting up SLURM_SAMPLES_DIR..."
    if [ -z "${SLURM_SAMPLES_DIR:-}" ]; then
        if [ -d "../cedana-samples/slurm" ]; then
            SLURM_SAMPLES_DIR="../cedana-samples/slurm"
        elif [ -d "/cedana-samples/slurm" ]; then
            SLURM_SAMPLES_DIR="/cedana-samples/slurm"
        elif [ -d "/tmp/cedana-samples/slurm" ]; then
            SLURM_SAMPLES_DIR="/tmp/cedana-samples/slurm"
        else
            if git clone --depth 1 https://github.com/cedana/cedana-samples.git /tmp/cedana-samples 2>/dev/null; then
                SLURM_SAMPLES_DIR="/tmp/cedana-samples/slurm"
            else
                SLURM_SAMPLES_DIR=""
            fi
        fi
    fi
    export SLURM_SAMPLES_DIR
    debug_log "SLURM_SAMPLES_DIR is set to $SLURM_SAMPLES_DIR"

    # Copy workloads into the cluster
    if [ -n "$SLURM_SAMPLES_DIR" ] && [ -d "$SLURM_SAMPLES_DIR" ]; then
        docker cp "$SLURM_SAMPLES_DIR" "$SLURM_CONTROLLER:/data/slurm-samples"
        debug_log "Copied slurm samples to cluster controller"
    fi
}
