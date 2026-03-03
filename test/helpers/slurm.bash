#!/usr/bin/env bash

########################
### Slurm Helpers    ###
########################

CEDANA_SLURM_DIR="${CEDANA_SLURM_DIR:-}"

# Job data directory for sbatch files and output
SLURM_DATA_DIR="${SLURM_DATA_DIR:-/data}"

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
# Cluster setup (pure bash, single-node, replicates ansible roles exactly)
##############################

setup_slurm_cluster() {
    local SLURM_VERSION="25.05.5"
    local SLURM_USER="slurm"
    local SLURM_GROUP="slurm"
    local SLURM_UID="981"
    local SLURM_GID="981"
    local MUNGE_USER="munge"
    local MUNGE_GROUP="munge"
    local SLURM_CONF_DIR="/etc/slurm"
    local SLURM_LOG_DIR="/var/log/slurm"
    local SLURM_SPOOL_DIR="/var/spool/slurm"
    local SLURM_STATE_DIR="/var/spool/slurm/ctld"

    debug_log "Setting up SLURM ${SLURM_VERSION} (pure bash, single-node)..."

    sudo bash -c 'printf "#!/bin/sh\nexit 101\n" > /usr/sbin/policy-rc.d && chmod +x /usr/sbin/policy-rc.d' 2>/dev/null || true

    debug_log "Creating slurm/munge users..."
    getent group  "$MUNGE_GROUP"  &>/dev/null || sudo groupadd --system "$MUNGE_GROUP"
    getent passwd "$MUNGE_USER"   &>/dev/null || sudo useradd  --gid "$MUNGE_GROUP" \
        --shell /sbin/nologin --no-create-home --system "$MUNGE_USER"
    getent group  "$SLURM_GROUP"  &>/dev/null || sudo groupadd --gid "$SLURM_GID" "$SLURM_GROUP"
    getent passwd "$SLURM_USER"   &>/dev/null || sudo useradd  --uid "$SLURM_UID" --gid "$SLURM_GROUP" \
        --shell /bin/bash --home /var/lib/slurm --create-home --system "$SLURM_USER"

    debug_log "Installing SLURM build dependencies..."
    sudo apt-get update -qq
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        gcc g++ make bzip2 wget \
        munge libmunge-dev \
        libreadline-dev libpam0g-dev libssl-dev \
        libhwloc-dev libnuma-dev liblua5.3-dev \
        libncurses-dev liblz4-dev libdbus-1-dev \
        libmariadb-dev libmariadb-dev-compat \
        python3

    debug_log "Configuring munge..."
    for d in /etc/munge /var/log/munge /var/lib/munge; do
        sudo mkdir -p "$d"
        sudo chown "${MUNGE_USER}:${MUNGE_GROUP}" "$d"
        sudo chmod 0700 "$d"
    done
    sudo mkdir -p /run/munge
    sudo chown "${MUNGE_USER}:${MUNGE_GROUP}" /run/munge
    sudo chmod 0755 /run/munge

    if [ ! -f /etc/munge/munge.key ]; then
        sudo /usr/sbin/create-munge-key -f
    fi
    sudo chown "${MUNGE_USER}:${MUNGE_GROUP}" /etc/munge/munge.key
    sudo chmod 0400 /etc/munge/munge.key

    _svc_start munge /usr/sbin/munged

    debug_log "Verifying munge..."
    local munge_ok=false
    for _i in $(seq 1 15); do
        if munge -n 2>/dev/null | unmunge &>/dev/null; then
            munge_ok=true; break
        fi
        sleep 1
    done
    if [ "$munge_ok" = false ]; then
        error_log "Munge self-test failed after 15s — aborting"
        sudo cat /var/log/munge/munged.log 2>/dev/null || true
        return 1
    fi
    debug_log "Munge OK"

    for dir in "$SLURM_CONF_DIR" "$SLURM_LOG_DIR" "$SLURM_SPOOL_DIR" "$SLURM_STATE_DIR"; do
        sudo mkdir -p "$dir"
        sudo chown "${SLURM_USER}:${SLURM_GROUP}" "$dir"
        sudo chmod 0755 "$dir"
    done
    sudo mkdir -p /usr/lib64/slurm /usr/lib/slurm /run/slurmd
    sudo chown "${SLURM_USER}:${SLURM_GROUP}" /run/slurmd

    if [ ! -f /usr/sbin/slurmctld ]; then
        local tarball="/tmp/slurm-${SLURM_VERSION}.tar.bz2"
        local srcdir="/tmp/slurm-${SLURM_VERSION}"

        debug_log "Downloading SLURM ${SLURM_VERSION}..."
        wget -q "https://download.schedmd.com/slurm/slurm-${SLURM_VERSION}.tar.bz2" -O "$tarball"
        tar -xjf "$tarball" -C /tmp

        debug_log "Configuring SLURM..."
        pushd "$srcdir" > /dev/null
        MYSQL_CONFIG=/usr/bin/mysql_config \
            ./configure --prefix=/usr \
                        --sysconfdir="$SLURM_CONF_DIR" \
                        --with-systemdsystemunitdir=/usr/lib/systemd/system \
            2>&1 | tail -5

        debug_log "Building SLURM (this takes a few minutes)..."
        make -j"$(nproc)" 2>&1 | tail -5
        sudo make install 2>&1 | tail -5
        popd > /dev/null

        sudo rm -rf "$srcdir" "$tarball"
        sudo ldconfig
        debug_log "SLURM ${SLURM_VERSION} installed"
    else
        debug_log "SLURM already installed, skipping build"
    fi

    local hostname
    hostname=$(hostname -s)
    local cpus sockets cores_per_socket threads_per_core mem_mb real_mem
    cpus=$(nproc)
    sockets=$(lscpu | awk '/^Socket\(s\):/ {print $NF}')
    cores_per_socket=$(lscpu | awk '/^Core\(s\) per socket:/ {print $NF}')
    threads_per_core=$(lscpu | awk '/^Thread\(s\) per core:/ {print $NF}')
    mem_mb=$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo)
    real_mem=$(( mem_mb * 95 / 100 ))

    debug_log "Generating slurm.conf for ${hostname} (${cpus} CPUs, ${real_mem} MB)..."
    sudo tee "$SLURM_CONF_DIR/slurm.conf" > /dev/null <<EOF
ClusterName=cedana-test-cluster
SlurmctldHost=${hostname}
AuthType=auth/munge
CryptoType=crypto/munge
PlugStackConfig=${SLURM_CONF_DIR}/plugstack.conf
ProctrackType=proctrack/cgroup
ReturnToService=2
SlurmctldPidFile=/run/slurmctld.pid
SlurmctldPort=6817
SlurmdPidFile=/run/slurmd/slurmd.pid
SlurmdPort=6818
SlurmdSpoolDir=${SLURM_SPOOL_DIR}
SlurmUser=${SLURM_USER}
StateSaveLocation=${SLURM_STATE_DIR}
TaskPlugin=task/affinity,task/cgroup

InactiveLimit=0
KillWait=30
MinJobAge=300
SlurmctldTimeout=120
SlurmdTimeout=300
Waittime=0

SchedulerType=sched/backfill
SelectType=select/cons_tres

JobCompType=jobcomp/none
JobAcctGatherFrequency=30
SlurmctldDebug=info
SlurmctldLogFile=${SLURM_LOG_DIR}/slurmctld.log
SlurmdDebug=info
SlurmdLogFile=${SLURM_LOG_DIR}/slurmd.log

NodeName=${hostname} CPUs=${cpus} Boards=1 SocketsPerBoard=${sockets} CoresPerSocket=${cores_per_socket} ThreadsPerCore=${threads_per_core} RealMemory=${real_mem}
PartitionName=debug Nodes=ALL Default=YES MaxTime=INFINITE State=UP
EOF
    sudo chown "${SLURM_USER}:${SLURM_GROUP}" "$SLURM_CONF_DIR/slurm.conf"

    sudo touch "$SLURM_CONF_DIR/plugstack.conf"
    sudo chown "${SLURM_USER}:${SLURM_GROUP}" "$SLURM_CONF_DIR/plugstack.conf"

    sudo tee "$SLURM_CONF_DIR/cgroup.conf" > /dev/null <<'EOF'
ConstrainCores=yes
ConstrainRAMSpace=yes
ConstrainSwapSpace=yes
ConstrainDevices=yes
EOF
    sudo chown "${SLURM_USER}:${SLURM_GROUP}" "$SLURM_CONF_DIR/cgroup.conf"

    sudo mkdir -p /usr/lib/systemd/system
    sudo tee /usr/lib/systemd/system/slurmctld.service > /dev/null <<'EOF'
[Unit]
Description=Slurm controller daemon
After=network.target munge.service
Requires=munge.service

[Service]
Type=forking
EnvironmentFile=-/etc/sysconfig/slurmctld
ExecStart=/usr/sbin/slurmctld $SLURMCTLD_OPTIONS
ExecReload=/bin/kill -HUP $MAINPID
PIDFile=/run/slurmctld.pid
LimitNOFILE=65536
LimitMEMLOCK=infinity
LimitSTACK=infinity

[Install]
WantedBy=multi-user.target
EOF

    sudo tee /usr/lib/systemd/system/slurmd.service > /dev/null <<'EOF'
[Unit]
Description=Slurm node daemon
After=network.target munge.service
Requires=munge.service

[Service]
Type=forking
EnvironmentFile=-/etc/default/slurmd
ExecStart=/usr/sbin/slurmd $SLURMD_OPTIONS
ExecReload=/bin/kill -HUP $MAINPID
PIDFile=/run/slurmd/slurmd.pid
RuntimeDirectory=slurmd
RuntimeDirectoryMode=0755
KillMode=process
LimitNOFILE=131072
LimitMEMLOCK=infinity
LimitSTACK=infinity
Delegate=yes

[Install]
WantedBy=multi-user.target
EOF

    debug_log "Starting slurmctld..."
    _svc_start slurmctld /usr/sbin/slurmctld

    debug_log "Starting slurmd..."
    _svc_start slurmd /usr/sbin/slurmd

    wait_for_slurm_ready 180
    debug_log "SLURM cluster is ready"
}

teardown_slurm_cluster() {
    debug_log "Stopping SLURM services..."
    _svc_stop slurmctld
    _svc_stop slurmd
    _svc_stop cedana-slurm
    sudo pkill -f 'cedana daemon start' 2>/dev/null || true
    sudo pkill -x munged 2>/dev/null || true
}

wait_for_slurm_ready() {
    local timeout=${1:-120}
    local elapsed=0

    debug_log "Waiting for SLURM to be ready..."

    while [ $elapsed -lt $timeout ]; do
        scontrol update nodename=ALL state=resume 2>/dev/null || true

        local node_state
        node_state=$(sinfo -h -o '%T' 2>/dev/null | head -1)
        debug_log "Node state: ${node_state:-<no response>}"

        if echo "$node_state" | grep -qiE 'idle|mixed|alloc'; then
            debug_log "SLURM nodes are ready"
            sinfo
            return 0
        fi
        sleep 5
        elapsed=$((elapsed + 5))
    done

    error_log "SLURM not ready after ${timeout}s"
    echo "=== sinfo ==="
    sinfo 2>/dev/null || echo "(sinfo unavailable)"
    echo "=== scontrol show nodes ==="
    scontrol show nodes 2>/dev/null || echo "(scontrol unavailable)"
    echo "=== processes ==="
    pgrep -xa 'slurmctld|slurmd|munged' 2>/dev/null || echo "(none running)"
    echo "=== slurmctld.log (last 40 lines) ==="
    sudo tail -40 /var/log/slurm/slurmctld.log 2>/dev/null || true
    echo "=== slurmd.log (last 40 lines) ==="
    sudo tail -40 /var/log/slurm/slurmd.log 2>/dev/null || true
    echo "=== munged.log (last 20 lines) ==="
    sudo tail -20 /var/log/munge/munged.log 2>/dev/null || true
    return 1
}

##############################
# Cedana Installation in Cluster
##############################

# Install cedana binary, plugins, and slurm binaries onto the host.
# Ansible provisions SLURM-only (--skip-tags cedana); this installs the freshly-built
# CI artifacts so the test exercises the actual build under test.
install_cedana_in_slurm() {
    debug_log "Installing cedana..."

    if command -v cedana &>/dev/null; then
        sudo cp "$(which cedana)" /usr/local/bin/cedana
        sudo chmod +x /usr/local/bin/cedana
    fi

    for lib in /usr/local/lib/libcedana-*.so; do
        [ -f "$lib" ] && sudo cp "$lib" "/usr/local/lib/$(basename "$lib")"
    done

    if command -v criu &>/dev/null; then
        sudo cp "$(which criu)" /usr/local/bin/criu
        sudo chmod +x /usr/local/bin/criu
    fi

    if [ -n "${CEDANA_SLURM_BIN:-}" ] && [ -f "$CEDANA_SLURM_BIN" ]; then
        sudo cp "$CEDANA_SLURM_BIN" /usr/local/bin/cedana-slurm
        sudo chmod +x /usr/local/bin/cedana-slurm
    fi

    for so in libslurm-cedana.so task_cedana.so cli_filter_cedana.so; do
        [ -f "/usr/local/lib/$so" ] || true  # already in place from artifact download
    done

    cedana plugin install criu gpu slurm 2>/dev/null || true
    cedana slurm setup 2>/dev/null || true

    # Add task/cedana to TaskPlugin in slurm.conf if not already there
    if ! grep -q 'task/cedana' /etc/slurm/slurm.conf 2>/dev/null; then
        sudo sed -i 's/^TaskPlugin=.*/&,task\/cedana/' /etc/slurm/slurm.conf
    fi

    sudo mkdir -p /etc/cedana
    cedana --init-config version 2>/dev/null || true

    if ! pgrep -f 'cedana daemon start' &>/dev/null; then
        debug_log "Starting cedana daemon..."
        sudo -E cedana daemon start --init-config 2>&1 &
        disown
        sleep 3
    fi

    _svc_stop slurmctld; _svc_stop slurmd
    _svc_start slurmctld "/usr/sbin/slurmctld"
    _svc_start slurmd    "/usr/sbin/slurmd"
    sleep 5

    debug_log "Cedana installed"
}

# Install and start cedana-slurm daemon.
start_cedana_slurm_daemon() {
    debug_log "Starting cedana-slurm daemon..."

    sudo install -m 0600 -o root /dev/null /etc/cedana-slurm.env
    printf 'CEDANA_URL=%s\nCEDANA_AUTH_TOKEN=%s\nRABBITMQ_URL=%s\nCEDANA_LOG_LEVEL=%s\n' \
        "${CEDANA_URL:-}" \
        "${CEDANA_AUTH_TOKEN:-}" \
        "${RABBITMQ_URL:-}" \
        "${CEDANA_LOG_LEVEL:-debug}" \
        | sudo tee /etc/cedana-slurm.env > /dev/null

    sudo tee /etc/systemd/system/cedana-slurm.service > /dev/null <<'UNIT'
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

    sudo systemctl daemon-reload 2>/dev/null || true
    _svc_start cedana-slurm /usr/local/bin/cedana-slurm daemon start

    sleep 3
    # Check if running
    if pgrep -f "cedana-slurm daemon" &>/dev/null; then
        debug_log "cedana-slurm daemon is running"
    else
        debug_log "WARNING: cedana-slurm daemon may not be running"
        sudo systemctl status cedana-slurm --no-pager 2>/dev/null || true
    fi
    debug_log "cedana-slurm daemon started"
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

    sudo mkdir -p "$SLURM_DATA_DIR"
    sudo cp "$sbatch_file" "${SLURM_DATA_DIR}/${filename}"

    local output
    output=$(sbatch --parsable "${SLURM_DATA_DIR}/${filename}" 2>&1)
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
        state=$(scontrol show job "$job_id" 2>/dev/null \
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
    scontrol show job "$job_id" 2>/dev/null \
        | grep -oP 'BatchHost=\K\S+' | head -1
}

get_slurm_job_info() {
    local job_id="$1"
    scontrol show job "$job_id" -o 2>/dev/null
}

cancel_slurm_job() {
    local job_id="$1"
    scancel "$job_id" 2>/dev/null || true
}

get_slurm_job_output() {
    local job_id="$1"
    local job_name="${2:-}"
    if [ -n "$job_name" ]; then
        cat "${SLURM_DATA_DIR}/${job_name}-${job_id}.out" 2>/dev/null || true
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
                new_job_id=$(squeue -h -o "%i" --sort=-V 2>/dev/null | head -1)
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
        squeue 2>/dev/null || true
        sinfo 2>/dev/null || true
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
        sudo mkdir -p "${SLURM_DATA_DIR}/slurm-samples"
        sudo cp -r "$SLURM_SAMPLES_DIR/." "${SLURM_DATA_DIR}/slurm-samples/"
        debug_log "Copied slurm samples to ${SLURM_DATA_DIR}/slurm-samples"
    fi
}
