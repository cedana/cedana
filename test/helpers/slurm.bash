#!/usr/bin/env bash

########################
### Slurm Helpers    ###
########################

CEDANA_SLURM_DIR="${CEDANA_SLURM_DIR:-}"

# Job data directory for sbatch files and output
SLURM_DATA_DIR="${SLURM_DATA_DIR:-/data}"

# Name of the SLURM controller Docker container (set by docker-deploy.sh)
SLURM_CONTROLLER_CONTAINER="${SLURM_CONTROLLER_CONTAINER:-slurm-controller}"

# Number of compute nodes — must match docker-deploy.sh COMPUTE_NODES
COMPUTE_NODES="${COMPUTE_NODES:-1}"

# Build compute node name list dynamically
_slurm_compute_containers() {
    local names=()
    for i in $(seq 1 "$COMPUTE_NODES"); do
        names+=("slurm-compute-$(printf '%02d' "$i")")
    done
    echo "${names[@]}"
}

# Run a command inside the SLURM controller container.
slurm_exec() {
    docker exec -i "$SLURM_CONTROLLER_CONTAINER" "$@"
}

##############################
# Service management helpers
##############################

# Start a daemon: tries systemctl, falls back to direct invocation.
_svc_start() {
    local name="$1"; shift
    local binary="$1"; shift
    local extra_args=("$@")

    sudo pkill -x "$(basename "$binary")" 2>/dev/null || true
    sleep 1

    if command -v systemctl &>/dev/null && \
       sudo systemctl daemon-reload 2>/dev/null && \
       sudo systemctl start "$name" 2>/dev/null; then
        sleep 1
        if sudo systemctl is-active --quiet "$name" 2>/dev/null; then
            debug_log "$name started via systemctl"
            return 0
        fi
    fi

    debug_log "Starting $name directly (no systemd)..."
    sudo "$binary" "${extra_args[@]}" &>/dev/null &
    local bg_pid=$!
    sleep 3

    if pgrep -f "$(basename "$binary")" &>/dev/null; then
        debug_log "$name is running"
        return 0
    fi
    error_log "$name failed to start"
    return 1
}

_svc_stop() {
    local name="$1"
    sudo systemctl stop "$name" 2>/dev/null || true
    sudo pkill -f "$name" 2>/dev/null || true
    sleep 1
}

##############################
# Cluster setup via docker-deploy.sh + ansible (SLURM only)
##############################

setup_slurm_cluster() {
    local ansible_dir="${CEDANA_SLURM_DIR}/ansible"

    info_log "Setting up SLURM cluster via docker-deploy.sh (SLURM only)..."

    if ! command -v docker &>/dev/null; then
        error_log "Docker CLI not found. Ensure /var/run/docker.sock is mounted into the CI container."
        return 1
    fi

    # Stream docker-deploy.sh output live to the terminal (fd 3 = direct output in BATS)
    pushd "$ansible_dir" > /dev/null
    ANSIBLE_SKIP_TAGS="cedana" bash docker-deploy.sh >&3 2>&1
    local rc=$?
    popd > /dev/null

    if [ $rc -ne 0 ]; then
        error_log "docker-deploy.sh failed (exit $rc)"
        return 1
    fi

    info_log "SLURM cluster is ready"
}

teardown_slurm_cluster() {
    debug_log "Tearing down SLURM cluster (Docker containers)..."
    docker rm -f slurm-controller $(seq 1 "$COMPUTE_NODES" | xargs -I{} printf 'slurm-compute-%02d ' {}) 2>/dev/null || true
    docker network rm slurm-net 2>/dev/null || true
}

wait_for_slurm_ready() {
    local timeout=${1:-120}
    local elapsed=0

    info_log "Waiting for SLURM nodes to be ready (timeout ${timeout}s)..."

    while [ $elapsed -lt $timeout ]; do
        local node_state
        node_state=$(slurm_exec sinfo -h -o '%T' 2>/dev/null | head -1)
        info_log "  [${elapsed}s] node state: ${node_state:-<no response>}"

        if echo "$node_state" | grep -qiE 'idle|mixed|alloc'; then
            debug_log "SLURM nodes are ready"
            slurm_exec sinfo
            return 0
        fi
        sleep 5
        elapsed=$((elapsed + 5))
    done

    error_log "SLURM not ready after ${timeout}s"
    echo "=== sinfo ==="
    slurm_exec sinfo 2>/dev/null || echo "(sinfo unavailable)"
    echo "=== scontrol show nodes ==="
    slurm_exec scontrol show nodes 2>/dev/null || echo "(scontrol unavailable)"
    echo "=== processes ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" pgrep -xa 'slurmctld|slurmd|munged' 2>/dev/null || echo "(none running)"
    echo "=== slurmctld.log (last 40 lines) ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" tail -40 /var/log/slurm/slurmctld.log 2>/dev/null || true
    echo "=== slurmd.log on compute nodes (last 40 lines) ==="
    for c in $(_slurm_compute_containers); do
        echo "--- $c ---"
        docker exec "$c" tail -40 /var/log/slurm/slurmd.log 2>/dev/null || echo "(log unavailable on $c)"
    done
    echo "=== munged.log (last 20 lines) ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" tail -20 /var/log/munge/munged.log 2>/dev/null || true
    return 1
}

##############################
# Cedana Installation in Cluster
##############################

# Install cedana binary, plugins, and slurm binaries into the SLURM Docker containers.
install_cedana_in_slurm() {
    info_log "Installing cedana into SLURM cluster containers..."

    local all_containers=("$SLURM_CONTROLLER_CONTAINER")
    local compute_containers=()
    # shellcheck disable=SC2207
    compute_containers=($(_slurm_compute_containers))
    all_containers+=("${compute_containers[@]}")

    info_log "Copying cedana + criu binaries into containers..."
    local cedana_bin criu_bin
    cedana_bin=$(which cedana 2>/dev/null) || { error_log "cedana binary not found in PATH"; return 1; }
    criu_bin=$(which criu 2>/dev/null) || { error_log "criu binary not found in PATH"; return 1; }

    for c in "${all_containers[@]}"; do
        docker cp "$cedana_bin" "${c}:/usr/local/bin/cedana" || { error_log "Failed to copy cedana into $c"; return 1; }
        docker exec "$c" chmod +x /usr/local/bin/cedana || { error_log "Failed to chmod cedana in $c"; return 1; }
        docker cp "$criu_bin" "${c}:/usr/local/bin/criu" || { error_log "Failed to copy criu into $c"; return 1; }
        docker exec "$c" chmod +x /usr/local/bin/criu || { error_log "Failed to chmod criu in $c"; return 1; }
    done

    info_log "Copying host-installed plugin libraries into containers..."
    for so in /usr/local/lib/libcedana-*.so; do
        [ -f "$so" ] || continue
        for c in "${all_containers[@]}"; do
            docker cp "$so" "${c}:/usr/local/lib/$(basename "$so")" || { error_log "Failed to copy $so into $c"; return 1; }
        done
    done

    info_log "Copying CI-built slurm/wlm artifacts into containers..."
    for so in task_cedana.so libslurm-cedana.so cli_filter_cedana.so; do
        local sopath="/usr/local/lib/${so}"
        [ -f "$sopath" ] || continue
        for c in "${all_containers[@]}"; do
            docker cp "$sopath" "${c}:/usr/local/lib/${so}" || { error_log "Failed to copy $so into $c"; return 1; }
        done
    done
    if [ -f "/usr/local/bin/cedana-slurm" ]; then
        for c in "${all_containers[@]}"; do
            docker cp /usr/local/bin/cedana-slurm "${c}:/usr/local/bin/cedana-slurm" || { error_log "Failed to copy cedana-slurm into $c"; return 1; }
            docker exec "$c" chmod +x /usr/local/bin/cedana-slurm || { error_log "Failed to chmod cedana-slurm in $c"; return 1; }
        done
    fi

    info_log "Configuring SLURM to load cedana plugins (setup.sh equivalent) on all nodes..."
    for c in "${all_containers[@]}"; do
        info_log "  SLURM plugin setup on $c:"
        docker exec -i "$c" bash << 'SETUP_EOF' >&3 2>&1 || { error_log "SLURM plugin setup failed on $c"; return 1; }
set -euo pipefail
PLUGIN_DIR=$(scontrol show config 2>/dev/null | grep PluginDir | awk '{print $3}')
if [ -z "$PLUGIN_DIR" ]; then
    echo "ERROR: Could not determine SLURM PluginDir" >&2
    exit 1
fi
echo "SLURM PluginDir: $PLUGIN_DIR"

chmod 755 /usr/local/lib/task_cedana.so /usr/local/lib/libslurm-cedana.so /usr/local/lib/cli_filter_cedana.so
cp /usr/local/lib/task_cedana.so /usr/local/lib/cli_filter_cedana.so "$PLUGIN_DIR/"
cp /usr/local/lib/libslurm-cedana.so "$PLUGIN_DIR/spank_cedana.so"
ldconfig

SLURM_CONF="${SLURM_CONF:-/etc/slurm/slurm.conf}"
PLUGSTACK_CONF=$(scontrol show config 2>/dev/null | grep PlugStackConfig | awk '{print $3}')
PLUGSTACK_CONF="${PLUGSTACK_CONF:-/etc/slurm/plugstack.conf}"

if grep -q "TaskPlugin=" "$SLURM_CONF" && ! grep -q "task/cedana" "$SLURM_CONF"; then
    sed -i 's|^\(TaskPlugin=.*\)|\1,task/cedana|' "$SLURM_CONF"
    echo "Added task/cedana to TaskPlugin"
fi
if ! grep -q "cli_filter/cedana" "$SLURM_CONF"; then
    echo "CliFilterPlugins=cli_filter/cedana" >> "$SLURM_CONF"
    echo "Added CliFilterPlugins=cli_filter/cedana"
fi
if ! grep -q "spank_cedana.so" "$PLUGSTACK_CONF" 2>/dev/null; then
    echo "required ${PLUGIN_DIR}/spank_cedana.so" >> "$PLUGSTACK_CONF"
    echo "Added spank_cedana.so to $PLUGSTACK_CONF"
fi
SETUP_EOF
    done

    info_log "Starting cedana daemon on all nodes..."
    for c in "${all_containers[@]}"; do
        docker exec "$c" mkdir -p /etc/cedana
        docker exec \
            -e CEDANA_URL="${CEDANA_URL:-}" \
            -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
            -e CEDANA_ADDRESS="/run/cedana.sock" \
            -e CEDANA_PROTOCOL="unix" \
            -e CEDANA_DB_REMOTE="true" \
            -e CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-info}" \
            -e CEDANA_CHECKPOINT_DIR="${CEDANA_CHECKPOINT_DIR:-cedana://}" \
            "$c" \
            bash -c "cedana --init-config version" || { error_log "cedana --init-config failed on $c"; return 1; }

        docker exec -d \
            -e CEDANA_URL="${CEDANA_URL:-}" \
            -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
            -e CEDANA_ADDRESS="/run/cedana.sock" \
            -e CEDANA_PROTOCOL="unix" \
            -e CEDANA_DB_REMOTE="true" \
            -e CEDANA_CLIENT_WAIT_FOR_READY="true" \
            -e CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-info}" \
            -e CEDANA_CHECKPOINT_DIR="${CEDANA_CHECKPOINT_DIR:-cedana://}" \
            "$c" \
            /usr/local/bin/cedana daemon start --init-config
    done
    sleep 5

    info_log "Waiting for cedana daemon socket on all nodes..."
    for c in "${all_containers[@]}"; do
        local waited=0
        while [ $waited -lt 30 ]; do
            if docker exec "$c" test -S /run/cedana.sock 2>/dev/null; then
                info_log "  $c: cedana socket ready (${waited}s)"
                break
            fi
            sleep 1
            waited=$((waited + 1))
        done
        if [ $waited -ge 30 ]; then
            info_log "  WARNING: cedana socket not ready on $c after 30s — proceeding anyway"
        fi
    done

    info_log "Restarting SLURM services to load task_cedana plugin..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" \
        bash -c "systemctl restart slurmctld || (pkill slurmctld; sleep 1; slurmctld)" \
        || { error_log "Failed to restart slurmctld on $SLURM_CONTROLLER_CONTAINER"; return 1; }
    for c in "${compute_containers[@]}"; do
        docker exec "$c" \
            bash -c "systemctl restart slurmd || (pkill slurmd; sleep 1; slurmd)" \
            || { error_log "Failed to restart slurmd on $c"; return 1; }
    done
    sleep 5

    wait_for_slurm_ready 180
    info_log "Cedana installed in SLURM cluster"
}

start_cedana_slurm_daemon() {
    debug_log "Starting cedana-slurm daemon in controller container..."

    if [ -n "${CEDANA_SLURM_BIN:-}" ] && [ -f "$CEDANA_SLURM_BIN" ]; then
        docker cp "$CEDANA_SLURM_BIN" "${SLURM_CONTROLLER_CONTAINER}:/usr/local/bin/cedana-slurm"
        docker exec "$SLURM_CONTROLLER_CONTAINER" chmod +x /usr/local/bin/cedana-slurm
    fi

    docker exec -d \
        -e CEDANA_URL="${CEDANA_URL:-}" \
        -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
        -e CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-debug}" \
        "$SLURM_CONTROLLER_CONTAINER" \
        bash -c 'cedana-slurm daemon start >/var/log/cedana-slurm.log 2>&1'

    sleep 3
    if docker exec "$SLURM_CONTROLLER_CONTAINER" pgrep -f 'cedana-slurm daemon' &>/dev/null; then
        debug_log "cedana-slurm daemon is running"
    else
        error_log "cedana-slurm daemon failed to start on $SLURM_CONTROLLER_CONTAINER"
        docker exec "$SLURM_CONTROLLER_CONTAINER" tail -20 /var/log/cedana-slurm.log
        return 1
    fi
}

##############################
# Slurm Job Management
##############################

# Submit an sbatch job to the SLURM cluster
# @param $1: Path to sbatch file (any path ending in .../slurm/cpu/foo.sbatch)
# Returns: SLURM job ID
slurm_submit_job() {
    local sbatch_file="$1"

    local rel_path filename container_chdir
    rel_path="${sbatch_file#*/slurm/}"
    filename="$(basename "$rel_path")"
    container_chdir="/data/cedana-samples/slurm/$(dirname "$rel_path")"
    debug_log "Submitting: cd $container_chdir && sbatch $filename"

    local output
    output=$(slurm_exec sbatch --parsable \
        --overcommit --cpus-per-task=1 --mem=0 \
        --chdir="${container_chdir}" \
        "${container_chdir}/${filename}" 2>&1)
    local exit_code=$?

    if [ $exit_code -ne 0 ]; then
        error_log "Failed to submit sbatch job: $output"
        return 1
    fi

    local job_id
    job_id=$(echo "$output" | tail -1 | tr -d '[:space:]')

    debug_log "Submitted job $container_chdir/$filename -> SLURM job ID: $job_id"
    echo "$job_id"
}

_dump_job_failure_info() {
    local job_id="${1:-}"

    echo "=== sacct (last 10 jobs) ==="
    slurm_exec sacct --noheader -a \
        --format=JobID,JobName,State,ExitCode,DerivedExitCode,Reason,NodeList,Submit,Start,End \
        -P 2>/dev/null | tail -10 || true

    if [ -n "$job_id" ]; then
        echo "=== scontrol show job $job_id ==="
        slurm_exec scontrol show job "$job_id" 2>/dev/null || true

        echo "=== job output files (compute nodes) ==="
        for c in $(_slurm_compute_containers); do
            for f in $(docker exec "$c" find "${SLURM_DATA_DIR}" \
                    -name "*-${job_id}.*" 2>/dev/null); do
                echo "--- $c:$f ---"
                docker exec "$c" cat "$f" 2>/dev/null || true
            done
        done
    fi

    echo "=== slurmctld.log (last 50 lines) ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" tail -50 /var/log/slurm/slurmctld.log 2>/dev/null || true

    echo "=== slurmd.log on compute nodes (last 50 lines) ==="
    for c in $(_slurm_compute_containers); do
        echo "--- $c ---"
        docker exec "$c" tail -50 /var/log/slurm/slurmd.log 2>/dev/null || echo "(unavailable)"
    done

    echo "=== cedana daemon log on compute nodes (last 30 lines) ==="
    for c in $(_slurm_compute_containers); do
        echo "--- $c ---"
        docker exec "$c" journalctl -u cedana --no-pager -n 30 2>/dev/null \
            || docker exec "$c" tail -30 /var/log/cedana.log 2>/dev/null \
            || echo "(no cedana log available)"
    done

    echo "=== cedana-slurm daemon log (controller, last 30 lines) ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" tail -30 /var/log/cedana-slurm.log 2>/dev/null || true
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
        state=$(slurm_exec scontrol show job "$job_id" 2>/dev/null \
            | grep -oP 'JobState=\K\S+' || echo "UNKNOWN")

        debug_log "Job $job_id state: $state (want: $target_state)"

        if [ "$state" = "$target_state" ]; then
            return 0
        fi

        case "$state" in
            COMPLETED|FAILED|CANCELLED|TIMEOUT|NODE_FAIL)
                if [ "$state" != "$target_state" ]; then
                    error_log "Job $job_id reached terminal state $state (expected $target_state)"
                    _dump_job_failure_info "$job_id"
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
    slurm_exec scontrol show job "$job_id" 2>/dev/null \
        | grep -oP 'BatchHost=\K\S+' | head -1
}

get_slurm_job_info() {
    local job_id="$1"
    slurm_exec scontrol show job "$job_id" -o 2>/dev/null
}

cancel_slurm_job() {
    local job_id="$1"
    slurm_exec scancel "$job_id" 2>/dev/null || true
}

get_slurm_job_output() {
    local job_id="$1"
    local job_name="${2:-}"
    if [ -n "$job_name" ]; then
        docker exec "$SLURM_CONTROLLER_CONTAINER" cat "${SLURM_DATA_DIR}/${job_name}-${job_id}.out" 2>/dev/null || true
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
                new_job_id=$(slurm_exec squeue -h -o "%i" --sort=-V 2>/dev/null | head -1)
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
        slurm_exec squeue 2>/dev/null || true
        slurm_exec sinfo 2>/dev/null || true
        _dump_job_failure_info "${job_id:-}"
        return 1
    fi

    return 0
}

##############################
# Samples Directory Setup
##############################

setup_slurm_samples() {
    local samples_root
    samples_root="$(dirname "${SLURM_SAMPLES_DIR:-}")"

    if [ -z "$SLURM_SAMPLES_DIR" ] || [ ! -d "$SLURM_SAMPLES_DIR" ]; then
        error_log "SLURM_SAMPLES_DIR not set or not found at ${SLURM_SAMPLES_DIR:-<unset>} (check cedana-samples checkout)"
        return 1
    fi

    info_log "Copying cedana-samples into cluster nodes..."
    for c in "$SLURM_CONTROLLER_CONTAINER" $(_slurm_compute_containers); do
        docker exec "$c" mkdir -p /data
        docker cp "$samples_root" "${c}:/data/" || {
            error_log "Failed to copy cedana-samples into container $c"
            return 1
        }
    done
    info_log "cedana-samples ready in all cluster nodes"
}
