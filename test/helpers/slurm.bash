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

##############################
# SLURM Accounting Setup
##############################

# Install and configure MySQL + slurmdbd for SLURM accounting
setup_slurm_accounting() {
    info_log "Setting up SLURM accounting (MySQL + slurmdbd)..."
    
    local mysql_root_password="${SLURM_MYSQL_ROOT_PASSWORD:-slurmroot123}"
    local slurm_db_name="${SLURM_DB_NAME:-slurm_acct_db}"
    local slurm_db_user="${SLURM_DB_USER:-slurm}"
    local slurm_db_pass="${SLURM_DB_PASSWORD:-slurmdb123}"
    
    info_log "Installing MariaDB on controller..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        apt-get update -qq && \
        DEBIAN_FRONTEND=noninteractive apt-get install -y -qq mariadb-server python3-pymysql && \
        mkdir -p /var/run/mysqld && \
        chown mysql:mysql /var/run/mysqld
    " || { error_log "Failed to install MariaDB on controller"; return 1; }
    
    info_log "Starting MariaDB..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        # Initialize MariaDB data directory if needed
        if [ ! -d /var/lib/mysql/mysql ]; then
            mysql_install_db --user=mysql --basedir=/usr --datadir=/var/lib/mysql 2>/dev/null || true
        fi
        
        # Start MariaDB normally
        mysqld_safe --bind-address=127.0.0.1 &
        
        # Wait for MariaDB to be ready
        for i in {1..30}; do
            if mysqladmin ping 2>/dev/null; then
                break
            fi
            sleep 1
        done
        
        # Set root password (fresh install has no password)
        mysql -u root -e \"ALTER USER 'root'@'localhost' IDENTIFIED BY '$mysql_root_password'; FLUSH PRIVILEGES;\" 2>/dev/null || \
        mysql -u root -p'$mysql_root_password' -e \"SELECT 1;\" 2>/dev/null || {
            echo 'Failed to set MariaDB root password'
            exit 1
        }
        
        sleep 2
    " || { error_log "Failed to start MariaDB"; return 1; }
    
    info_log "Creating slurm accounting database..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        mysql -u root -p'$mysql_root_password' -e \"
            CREATE DATABASE IF NOT EXISTS $slurm_db_name;
            CREATE USER IF NOT EXISTS '$slurm_db_user'@'localhost' IDENTIFIED BY '$slurm_db_pass';
            CREATE USER IF NOT EXISTS '$slurm_db_user'@'127.0.0.1' IDENTIFIED BY '$slurm_db_pass';
            GRANT ALL PRIVILEGES ON $slurm_db_name.* TO '$slurm_db_user'@'localhost';
            GRANT ALL PRIVILEGES ON $slurm_db_name.* TO '$slurm_db_user'@'127.0.0.1';
            FLUSH PRIVILEGES;
        \"
    " || { error_log "Failed to create slurm database"; return 1; }
    
    info_log "Installing slurmdbd..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        apt-get install -y -qq slurmdbd || apt-get install -y -qq slurm-slurmdbd
    " || { error_log "Failed to install slurmdbd"; return 1; }
    
    info_log "Creating slurmdbd.conf..."
    docker exec -i "$SLURM_CONTROLLER_CONTAINER" bash << 'DBD_EOF' || { error_log "Failed to create slurmdbd.conf"; return 1; }
cat > /etc/slurm/slurmdbd.conf << 'EOF'
# SLURM DBD Configuration
AuthType=auth/munge
DbdAddr=localhost
DbdHost=localhost
DbdPort=6819
SlurmUser=slurm
DebugLevel=4
LogFile=/var/log/slurm/slurmdbd.log
PidFile=/var/run/slurmdbd.pid

# Database configuration
StorageType=accounting_storage/mysql
StorageHost=localhost
StoragePort=3306
StorageUser=slurm
StoragePass=slurmdb123
StorageLoc=slurm_acct_db
EOF

chown slurm:slurm /etc/slurm/slurmdbd.conf
chmod 600 /etc/slurm/slurmdbd.conf
DBD_EOF
    
    info_log "Starting slurmdbd..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        mkdir -p /var/run/slurmdbd /var/log/slurm
        chown slurm:slurm /var/run/slurmdbd
        slurmdbd -D &
        sleep 5
        
        # Check if slurmdbd is running
        if ! pgrep -x slurmdbd >/dev/null; then
            echo 'ERROR: slurmdbd failed to start'
            cat /var/log/slurm/slurmdbd.log 2>/dev/null | tail -20 || true
            exit 1
        fi
        
        # Wait for slurmdbd port to be ready
        for i in {1..10}; do
            if nc -z localhost 6819 2>/dev/null; then
                echo 'slurmdbd is ready on port 6819'
                break
            fi
            sleep 1
        done
    " || { error_log "Failed to start slurmdbd"; return 1; }

    info_log "Configuring slurm.conf for database..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        # Add accounting storage type to base conf so sacctmgr can read it
        if ! grep -q 'AccountingStorageType' /etc/slurm/slurm.conf; then
            echo 'AccountingStorageType=accounting_storage/slurmdbd' >> /etc/slurm/slurm.conf
        fi
        if ! grep -q 'AccountingStorageHost' /etc/slurm/slurm.conf; then
            echo 'AccountingStorageHost=localhost' >> /etc/slurm/slurm.conf
        fi
        if ! grep -q 'AccountingStoragePort' /etc/slurm/slurm.conf; then
            echo 'AccountingStoragePort=6819' >> /etc/slurm/slurm.conf
        fi
    " || { error_log "Failed to configure slurm.conf"; return 1; }
    
    info_log "Ensuring slurm spool directories exist..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        mkdir -p /var/spool/slurmctld /var/spool/slurmd
        chown slurm:slurm /var/spool/slurmctld /var/spool/slurmd
    " || true

    info_log "Starting slurmctld initially (without enforcement)..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        systemctl restart slurmctld || (pkill slurmctld; sleep 1; slurmctld)
        sleep 5
    " || { error_log "Failed to start initial slurmctld"; return 1; }

    info_log "Initializing accounting database..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        # Wait until slurmdbd is fully initialized and accepting sacctmgr connections
        slurmdbd_ready=false
        for i in {1..30}; do
            if sacctmgr show cluster 2>/dev/null; then
                slurmdbd_ready=true
                break
            fi
            sleep 2
        done
        
        if [ \"\$slurmdbd_ready\" = false ]; then
            echo \"Failed to connect to slurmdbd via sacctmgr after 60 seconds\" >&2
            exit 1
        fi
        
        # Add cluster to accounting
        sacctmgr -i add cluster cluster 2>/dev/null || true
        
        # Create default account
        sacctmgr -i add account default Description='Default Account' 2>/dev/null || true
        
        # Add root user
        sacctmgr -i add user root Account=default AdminLevel=Admin 2>/dev/null || true
    " || { error_log "Failed to initialize accounting database"; return 1; }

    info_log "Enforcing accounting in slurm.conf..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        # Enable job accounting gathering
        if ! grep -q 'JobAcctGatherType' /etc/slurm/slurm.conf; then
            echo 'JobAcctGatherType=jobacct_gather/linux' >> /etc/slurm/slurm.conf
        fi
        
        # Set accounting storage enforce
        if ! grep -q 'AccountingStorageEnforce' /etc/slurm/slurm.conf; then
            echo 'AccountingStorageEnforce=associations,limits,qos' >> /etc/slurm/slurm.conf
        fi
    " || { error_log "Failed to update slurm.conf for accounting"; return 1; }

    info_log "Restarting slurmctld to apply enforcement..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        systemctl restart slurmctld || (pkill slurmctld; sleep 1; slurmctld)
        sleep 5
    " || { error_log "Failed to restart slurmctld"; return 1; }
    
    info_log "SLURM accounting setup complete"
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

    info_log "Installing CRIU runtime dependencies in containers..."
    for c in "${all_containers[@]}"; do
        docker exec "$c" bash -c "apt-get update -qq && apt-get install -y -qq libprotobuf-c1 libnet1 libgnutls30 libnl-3-200 libbsd0 libcap2 python3 python3-pip" || {
            error_log "Failed to install dependencies in $c"
            return 1
        }
    done

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

    info_log "Configuring SLURM to load cedana plugins on controller..."
    docker exec -i "$SLURM_CONTROLLER_CONTAINER" bash << 'SETUP_EOF' >&3 2>&1 || { error_log "SLURM plugin setup failed on controller"; return 1; }
set -euo pipefail
PLUGIN_DIR=$(scontrol show config 2>/dev/null | grep PluginDir | awk '{print $3}' || true)
if [ -z "$PLUGIN_DIR" ]; then
    echo "ERROR: Could not determine SLURM PluginDir" >&2
    exit 1
fi
echo "SLURM PluginDir: $PLUGIN_DIR"

for f in /usr/local/lib/task_cedana.so /usr/local/lib/libslurm-cedana.so /usr/local/lib/cli_filter_cedana.so; do
    if [ -f "$f" ]; then chmod 755 "$f"; fi
done
for f in /usr/local/lib/task_cedana.so /usr/local/lib/cli_filter_cedana.so; do
    if [ -f "$f" ]; then cp "$f" "$PLUGIN_DIR/"; fi
done
if [ -f /usr/local/lib/libslurm-cedana.so ]; then cp /usr/local/lib/libslurm-cedana.so "$PLUGIN_DIR/spank_cedana.so"; fi
ldconfig

SLURM_CONF="${SLURM_CONF:-/etc/slurm/slurm.conf}"
PLUGSTACK_CONF=$(scontrol show config 2>/dev/null | grep PlugStackConfig | awk '{print $3}' || true)
PLUGSTACK_CONF="${PLUGSTACK_CONF:-/etc/slurm/plugstack.conf}"

if grep -q "TaskPlugin=" "$SLURM_CONF" && ! grep -q "task/cedana" "$SLURM_CONF"; then
    sed -i 's|^\(TaskPlugin=.*\)|\1,task/cedana|' "$SLURM_CONF"
    echo "Added task/cedana to TaskPlugin"
fi
if ! grep -q "cli_filter/cedana" "$SLURM_CONF"; then
    echo "CliFilterPlugins=cli_filter/cedana" >> "$SLURM_CONF"
    echo "Added CliFilterPlugins=cli_filter/cedana"
fi
if [ -f /usr/local/lib/libslurm-cedana.so ]; then
    if ! grep -q "spank_cedana.so" "$PLUGSTACK_CONF" 2>/dev/null; then
        echo "required ${PLUGIN_DIR}/spank_cedana.so" >> "$PLUGSTACK_CONF"
        echo "Added spank_cedana.so to $PLUGSTACK_CONF"
    fi
fi
SETUP_EOF

    # Copy plugins to compute nodes (but don't configure - controller handles config)
    info_log "Copying cedana plugins to compute nodes..."
    for c in "${compute_containers[@]}"; do
        docker exec -i "$c" bash << 'COMPUTE_EOF' >&3 2>&1 || { error_log "Plugin setup failed on $c"; return 1; }
set -euo pipefail
PLUGIN_DIR=$(scontrol show config 2>/dev/null | grep PluginDir | awk '{print $3}' || true)
if [ -z "$PLUGIN_DIR" ]; then
    echo "ERROR: Could not determine SLURM PluginDir on $c" >&2
    exit 1
fi
for f in /usr/local/lib/task_cedana.so /usr/local/lib/libslurm-cedana.so /usr/local/lib/cli_filter_cedana.so; do
    if [ -f "$f" ]; then chmod 755 "$f"; fi
done
for f in /usr/local/lib/task_cedana.so /usr/local/lib/cli_filter_cedana.so; do
    if [ -f "$f" ]; then cp "$f" "$PLUGIN_DIR/"; fi
done
if [ -f /usr/local/lib/libslurm-cedana.so ]; then cp /usr/local/lib/libslurm-cedana.so "$PLUGIN_DIR/spank_cedana.so"; fi
echo "Plugins installed on $(hostname)"
COMPUTE_EOF
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
            bash -c "/usr/local/bin/cedana daemon start --init-config >/var/log/cedana.log 2>&1"
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

    info_log "=== SPANK Plugin Diagnostics ==="
    for c in "${compute_containers[@]}"; do
        info_log "--- Checking SPANK plugin on $c ---"
        docker exec "$c" bash -c '
            echo "PlugStack config:"
            cat /etc/slurm/plugstack.conf 2>/dev/null || echo "NOT FOUND"
            echo ""
            echo "SPANK plugin file:"
            ls -la /usr/lib/slurm/spank_cedana.so 2>/dev/null || echo "NOT FOUND"
            echo ""
            echo "Plugin dependencies:"
            ldd /usr/lib/slurm/spank_cedana.so 2>/dev/null || echo "ldd failed"
            echo ""
            echo "SLURM config:"
            scontrol show config 2>/dev/null | grep -i plugstack || echo "No PlugStackConfig in slurm.conf"
        ' >&3 || true
    done
    info_log "=== End SPANK Plugin Diagnostics ==="

    info_log "Starting cedana-slurm daemon on compute nodes..."
    for c in "${compute_containers[@]}"; do
        docker exec -d \
            -e CEDANA_URL="${CEDANA_URL:-}" \
            -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
            -e CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-debug}" \
            "$c" \
            bash -c '/usr/local/bin/cedana-slurm daemon start >/var/log/cedana-slurm.log 2>&1' || {
            error_log "Failed to start cedana-slurm daemon on $c"
            return 1
        }
        info_log "cedana-slurm daemon started on $c"
    done

    sleep 3
    for c in "${compute_containers[@]}"; do
        if docker exec "$c" pgrep -f 'cedana-slurm daemon' &>/dev/null; then
            info_log "cedana-slurm daemon is running on $c"
        else
            error_log "cedana-slurm daemon failed to start on $c"
            docker exec "$c" tail -20 /var/log/cedana-slurm.log
            return 1
        fi
    done

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

    local rel_path container_dir container_file
    rel_path="${sbatch_file#*/slurm/}"
    container_dir="/data/cedana-samples/slurm/$(dirname "$rel_path")"
    container_file="$(basename "$rel_path")"
    debug_log "Submitting: cd $container_dir && sbatch $container_file"

    local output
    output=$(slurm_exec bash -c "cd '$container_dir' && sbatch --parsable --overcommit --cpus-per-task=1 --mem=0 '$container_file'" 2>&1)
    local exit_code=$?

    if [ $exit_code -ne 0 ]; then
        error_log "Failed to submit sbatch job: $output"
        return 1
    fi

    local job_id
    job_id=$(echo "$output" | tail -1 | cut -d';' -f1 | tr -d '[:space:]')

    debug_log "Submitted job $container_dir/$container_file -> SLURM job ID: $job_id"
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
        docker exec "$c" tail -30 /var/log/cedana.log 2>/dev/null \
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

                info_log "Checkpointing slurm job $job_id via propagator..."
                local slurm_job_name
                slurm_job_name=$(slurm_exec scontrol show job "$job_id" 2>/dev/null \
                    | grep -o 'JobName=[^ ]*' | cut -d= -f2)

                local checkpoint_output
                checkpoint_output=$(checkpoint_slurm_job "$slurm_job_name")
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
    info_log "Cloning cedana-samples into cluster nodes..."

    for c in "$SLURM_CONTROLLER_CONTAINER" $(_slurm_compute_containers); do
        docker exec "$c" bash -c '
            apt-get install -y -qq git 2>/dev/null
            rm -rf /data/cedana-samples
            mkdir -p /data
            git clone --depth 1 https://github.com/cedana/cedana-samples.git /data/cedana-samples
        ' || {
            error_log "Failed to clone cedana-samples into container $c"
            return 1
        }

        info_log "Verifying cloned structure in container $c:"
        docker exec "$c" ls -la /data/cedana-samples/ 2>&1 | head -20 >&3 || true
        docker exec "$c" test -d /data/cedana-samples/slurm/cpu && \
            info_log "  slurm/cpu directory found in $c" || \
            error_log "  slurm/cpu directory NOT found in $c"
    done
    info_log "cedana-samples ready in all cluster nodes"
}
