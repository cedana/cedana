#!/usr/bin/env bash
# shellcheck disable=SC2016

########################
### Slurm Helpers    ###
########################

CEDANA_SLURM_DIR="${CEDANA_SLURM_DIR:-}"

# Job data directory for sbatch files and output
SLURM_DATA_DIR="${SLURM_DATA_DIR:-/data}"

# Name of the SLURM controller Docker container
SLURM_CONTROLLER_CONTAINER="${SLURM_CONTROLLER_CONTAINER:-slurm-controller}"

# Number of compute nodes — must match docker-deploy.sh COMPUTE_NODES
COMPUTE_NODES="${COMPUTE_NODES:-1}"

##############################
# Internal Helpers
##############################

# Build list of compute container names
_slurm_compute_containers() {
    local names=()
    for i in $(seq 1 "$COMPUTE_NODES"); do
        names+=("slurm-compute-$(printf '%02d' "$i")")
    done
    echo "${names[@]}"
}

# Run a command inside the SLURM controller container
slurm_exec() {
    docker exec -i "$SLURM_CONTROLLER_CONTAINER" "$@"
}

# Wait for a TCP port to be open inside a container
# Usage: _wait_for_port <container> <port> <timeout_seconds>
_wait_for_port() {
    local container="$1"
    local port="$2"
    local timeout="${3:-30}"
    local elapsed=0

    while [ "$elapsed" -lt "$timeout" ]; do
        if docker exec "$container" bash -c "nc -z localhost $port" 2>/dev/null; then
            return 0
        fi
        sleep 2
        elapsed=$((elapsed + 2))
    done

    error_log "Port $port not open in $container after ${timeout}s"
    return 1
}

# Append a key=value line to slurm.conf if the key is not already present
_slurm_conf_set() {
    local container="$1"
    local key="$2"
    local value="$3"
    docker exec "$container" bash -c "
        grep -q '^${key}=' /etc/slurm/slurm.conf 2>/dev/null || \
            echo '${key}=${value}' >> /etc/slurm/slurm.conf
    "
}

##############################
# Service Management
##############################

# Start/restart a SLURM service inside a container.
# Tries systemctl first, falls back to killing and re-launching the binary directly.
# Usage: _svc_restart <container> <service_name> <binary_path> [extra_args...]
_svc_restart() {
    local container="$1"
    local name="$2"
    local binary="$3"
    shift 3
    local extra_args=("$@")

    debug_log "Restarting $name in $container..."

    # Try systemctl first
    if docker exec "$container" bash -c \
        "systemctl restart '$name' 2>/dev/null && systemctl is-active --quiet '$name'" 2>/dev/null; then
        debug_log "$name started via systemctl in $container"
        return 0
    fi

    # Fall back: kill existing, launch directly
    docker exec "$container" bash -c "pkill -x '$(basename "$binary")' 2>/dev/null || true; sleep 1"
    docker exec -d "$container" "$binary" "${extra_args[@]}"
    sleep 3

    if docker exec "$container" pgrep -x "$(basename "$binary")" &>/dev/null; then
        debug_log "$name started directly in $container"
        return 0
    fi

    error_log "$name failed to start in $container"
    return 1
}

##############################
# Cluster Setup
##############################

setup_slurm_cluster() {
    local ansible_dir="${CEDANA_SLURM_DIR}/ansible"

    info_log "Setting up SLURM cluster via docker-deploy.sh..."

    if ! command -v docker &>/dev/null; then
        error_log "Docker CLI not found. Ensure /var/run/docker.sock is mounted."
        return 1
    fi

    pushd "$ansible_dir" >/dev/null
    ANSIBLE_SKIP_TAGS="cedana" bash docker-deploy.sh >&3 2>&1
    local rc=$?
    popd >/dev/null

    if [ "$rc" -ne 0 ]; then
        error_log "docker-deploy.sh failed (exit $rc)"
        return 1
    fi

    info_log "SLURM cluster is ready"
}

teardown_slurm_cluster() {
    debug_log "Tearing down SLURM cluster (Docker containers)..."
    # shellcheck disable=SC2046
    docker rm -f "$SLURM_CONTROLLER_CONTAINER" \
        $(seq 1 "$COMPUTE_NODES" | xargs -I{} printf 'slurm-compute-%02d ' {}) 2>/dev/null || true
    docker network rm slurm-net 2>/dev/null || true
}

##############################
# SLURM Accounting Setup
##############################

# Full setup of MariaDB + slurmdbd for SLURM job accounting.
# Environment variables (all have sensible defaults):
#   SLURM_MYSQL_ROOT_PASSWORD  - MariaDB root password
#   SLURM_DB_NAME              - Accounting database name
#   SLURM_DB_USER              - DB user for slurmdbd
#   SLURM_DB_PASSWORD          - DB password for slurmdbd
setup_slurm_accounting() {
    info_log "Setting up SLURM accounting (MariaDB + slurmdbd)..."

    local mysql_root_password="${SLURM_MYSQL_ROOT_PASSWORD:-slurmroot123}"
    local slurm_db_name="${SLURM_DB_NAME:-slurm_acct_db}"
    local slurm_db_user="${SLURM_DB_USER:-slurm}"
    local slurm_db_pass="${SLURM_DB_PASSWORD:-slurmdb123}"

    # -------------------------------------------------------------------------
    # Step 1: Install and configure MariaDB
    # -------------------------------------------------------------------------
    info_log "[1/7] Installing MariaDB..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        apt-get update -qq
        DEBIAN_FRONTEND=noninteractive apt-get install -y -qq mariadb-server python3-pymysql netcat-openbsd
        mkdir -p /var/run/mysqld
        chown mysql:mysql /var/run/mysqld
    " || {
        error_log "Failed to install MariaDB"
        return 1
    }

    # Tune innodb settings — slurmdbd warns loudly without these
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        cat >> /etc/mysql/mariadb.conf.d/50-server.cnf << 'EOF'
innodb_buffer_pool_size = 128M
innodb_lock_wait_timeout = 900
EOF
    " || true # non-fatal; defaults still work

    # -------------------------------------------------------------------------
    # Step 2: Start MariaDB and set root password
    # -------------------------------------------------------------------------
    info_log "[2/7] Starting MariaDB..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        if [ ! -d /var/lib/mysql/mysql ]; then
            mysql_install_db --user=mysql --basedir=/usr --datadir=/var/lib/mysql 2>/dev/null || true
        fi
        mysqld_safe --bind-address=127.0.0.1 --skip-networking=0 &
        # Wait up to 30s for MariaDB to accept connections
        for i in \$(seq 1 30); do
            mysqladmin ping --silent 2>/dev/null && break
            sleep 1
        done
        mysqladmin ping --silent 2>/dev/null || { echo 'MariaDB did not start'; exit 1; }
    " || {
        error_log "Failed to start MariaDB"
        return 1
    }

    # Set root password — handle both fresh installs (no password) and already-set
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        mysql -u root --connect-expired-password -e \"
            ALTER USER 'root'@'localhost' IDENTIFIED BY '${mysql_root_password}';
            FLUSH PRIVILEGES;
        \" 2>/dev/null || \
        mysql -u root -p'${mysql_root_password}' -e 'SELECT 1;' 2>/dev/null || {
            echo 'ERROR: Cannot authenticate to MariaDB as root' >&2
            exit 1
        }
    " || {
        error_log "Failed to configure MariaDB root password"
        return 1
    }

    # -------------------------------------------------------------------------
    # Step 3: Create slurm accounting database and user
    # -------------------------------------------------------------------------
    info_log "[3/7] Creating SLURM accounting database..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        mysql -u root -p'${mysql_root_password}' << 'SQL'
CREATE DATABASE IF NOT EXISTS \`${slurm_db_name}\`;
CREATE USER IF NOT EXISTS '${slurm_db_user}'@'localhost' IDENTIFIED BY '${slurm_db_pass}';
CREATE USER IF NOT EXISTS '${slurm_db_user}'@'127.0.0.1' IDENTIFIED BY '${slurm_db_pass}';
GRANT ALL PRIVILEGES ON \`${slurm_db_name}\`.* TO '${slurm_db_user}'@'localhost';
GRANT ALL PRIVILEGES ON \`${slurm_db_name}\`.* TO '${slurm_db_user}'@'127.0.0.1';
FLUSH PRIVILEGES;
SQL
    " || {
        error_log "Failed to create SLURM accounting database"
        return 1
    }

    # -------------------------------------------------------------------------
    # Step 4: Verify slurmdbd binary is present (installed from source by ansible)
    # -------------------------------------------------------------------------

    info_log "[4/7] Verifying slurmdbd binary (must be pre-installed from source)..."
    if ! docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "test -x /usr/sbin/slurmdbd" 2>/dev/null; then
        error_log "slurmdbd binary not found at /usr/sbin/slurmdbd"
        error_log "Expected it to be installed by the ansible source build (setup_slurm_cluster)"
        return 1
    fi

    local slurmdbd_ver
    slurmdbd_ver=$(docker exec "$SLURM_CONTROLLER_CONTAINER" \
        bash -c "/usr/sbin/slurmdbd -V 2>&1 | awk '{print \$2}'" 2>/dev/null || true)
    info_log "slurmdbd binary present at /usr/sbin/slurmdbd (version: ${slurmdbd_ver:-unknown})"

    # -------------------------------------------------------------------------
    # Step 5: Write slurmdbd.conf and update slurm.conf
    #
    # Both configs are written BEFORE starting any SLURM daemon so that
    # slurmctld picks up accounting settings on its very first start.
    # -------------------------------------------------------------------------
    info_log "[5/7] Writing configuration files..."

    # slurmdbd.conf — note: NOT single-quoted heredoc so variables expand
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        mkdir -p /etc/slurm /var/log/slurm /var/run/slurmdbd
        cat > /etc/slurm/slurmdbd.conf << EOF
# slurmdbd configuration — generated by setup_slurm_accounting
AuthType=auth/munge
DbdAddr=localhost
DbdHost=localhost
DbdPort=6819
SlurmUser=slurm
DebugLevel=4
LogFile=/var/log/slurm/slurmdbd.log
PidFile=/var/run/slurmdbd/slurmdbd.pid

StorageType=accounting_storage/mysql
StorageHost=localhost
StoragePort=3306
StorageUser=${slurm_db_user}
StoragePass=${slurm_db_pass}
StorageLoc=${slurm_db_name}
EOF
        chown slurm:slurm /etc/slurm/slurmdbd.conf
        chmod 600 /etc/slurm/slurmdbd.conf
        chown slurm:slurm /var/run/slurmdbd
    " || {
        error_log "Failed to write slurmdbd.conf"
        return 1
    }

    # slurm.conf — append accounting keys only if not already present
    _slurm_conf_set "$SLURM_CONTROLLER_CONTAINER" "AccountingStorageType" "accounting_storage/slurmdbd"
    _slurm_conf_set "$SLURM_CONTROLLER_CONTAINER" "AccountingStorageHost" "localhost"
    _slurm_conf_set "$SLURM_CONTROLLER_CONTAINER" "AccountingStoragePort" "6819"
    _slurm_conf_set "$SLURM_CONTROLLER_CONTAINER" "JobAcctGatherType" "jobacct_gather/linux"
    # Enforcement is added later, after the cluster/account/user records exist

    # -------------------------------------------------------------------------
    # Step 6: Start slurmdbd and wait for it to be ready
    # -------------------------------------------------------------------------
    info_log "[6/7] Starting slurmdbd..."
    docker exec -d "$SLURM_CONTROLLER_CONTAINER" slurmdbd -D

    # Give it a moment to initialise the DB schema, then wait for the port
    sleep 5
    _wait_for_port "$SLURM_CONTROLLER_CONTAINER" 6819 60 ||
        {
            error_log "slurmdbd did not open port 6819 in time"
            docker exec "$SLURM_CONTROLLER_CONTAINER" \
                tail -30 /var/log/slurm/slurmdbd.log 2>/dev/null || true
            return 1
        }

    # -------------------------------------------------------------------------
    # Step 7: Start slurmctld, seed the accounting DB, then enforce it
    # -------------------------------------------------------------------------
    info_log "[7/7] Starting slurmctld and seeding accounting records..."

    # Ensure spool dirs exist before first start
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        mkdir -p /var/spool/slurmctld /var/spool/slurmd
        chown slurm:slurm /var/spool/slurmctld /var/spool/slurmd
    " || true

    _svc_restart "$SLURM_CONTROLLER_CONTAINER" slurmctld /usr/sbin/slurmctld ||
        {
            error_log "Failed to start slurmctld"
            return 1
        }
    sleep 5

    # Poll sacctmgr — slurmctld must register with slurmdbd before this works
    info_log "Waiting for sacctmgr to reach slurmdbd (up to 90s)..."
    local slurmdbd_ready=false
    for i in $(seq 1 45); do
        if slurm_exec sacctmgr show cluster -n 2>/dev/null; then
            slurmdbd_ready=true
            break
        fi
        sleep 2
    done

    if [ "$slurmdbd_ready" = false ]; then
        error_log "sacctmgr could not reach slurmdbd after 90 seconds"
        error_log "--- slurmdbd.log (last 30 lines) ---"
        docker exec "$SLURM_CONTROLLER_CONTAINER" tail -30 /var/log/slurm/slurmdbd.log 2>/dev/null || true
        error_log "--- slurmctld.log (last 30 lines) ---"
        docker exec "$SLURM_CONTROLLER_CONTAINER" tail -30 /var/log/slurm/slurmctld.log 2>/dev/null || true
        return 1
    fi

    # Seed cluster, account and admin user
    slurm_exec sacctmgr -i add cluster cluster 2>/dev/null || true
    slurm_exec sacctmgr -i add account default \
        Description="Default Account" Organization="default" 2>/dev/null || true
    slurm_exec sacctmgr -i add user root \
        Account=default AdminLevel=Admin 2>/dev/null || true

    # Now it is safe to enforce — cluster/account records exist
    _slurm_conf_set "$SLURM_CONTROLLER_CONTAINER" \
        "AccountingStorageEnforce" "associations,limits,qos"

    # Final slurmctld restart to pick up enforcement
    _svc_restart "$SLURM_CONTROLLER_CONTAINER" slurmctld /usr/sbin/slurmctld ||
        {
            error_log "Failed to restart slurmctld with accounting enforcement"
            return 1
        }
    sleep 5

    info_log "SLURM accounting setup complete"
}

##############################
# Cluster Readiness
##############################

wait_for_slurm_ready() {
    local timeout="${1:-120}"
    local elapsed=0

    info_log "Waiting for SLURM nodes to be ready (timeout ${timeout}s)..."

    while [ "$elapsed" -lt "$timeout" ]; do
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

    error_log "SLURM not ready after ${timeout}s — dumping diagnostics"
    echo "=== sinfo ==="
    slurm_exec sinfo 2>/dev/null || echo "(sinfo unavailable)"
    echo "=== scontrol show nodes ==="
    slurm_exec scontrol show nodes 2>/dev/null || echo "(scontrol unavailable)"
    echo "=== running SLURM processes ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" \
        pgrep -xa 'slurmctld|slurmd|slurmdbd|munged' 2>/dev/null || echo "(none)"
    echo "=== slurmctld.log (last 40 lines) ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" \
        tail -40 /var/log/slurm/slurmctld.log 2>/dev/null || true
    echo "=== slurmd.log on compute nodes (last 40 lines) ==="
    for c in $(_slurm_compute_containers); do
        echo "--- $c ---"
        docker exec "$c" tail -40 /var/log/slurm/slurmd.log 2>/dev/null ||
            echo "(log unavailable on $c)"
    done
    echo "=== munged.log (last 20 lines) ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" \
        tail -20 /var/log/munge/munged.log 2>/dev/null || true
    return 1
}

##############################
# Cedana Installation
##############################

# Install cedana binary, plugins, and slurm binaries into all cluster containers.
install_cedana_in_slurm() {
    info_log "Installing Cedana into SLURM cluster containers..."

    local all_containers=("$SLURM_CONTROLLER_CONTAINER")
    local compute_containers=()
    # shellcheck disable=SC2207
    compute_containers=($(_slurm_compute_containers))
    all_containers+=("${compute_containers[@]}")

    # -------------------------------------------------------------------------
    # Runtime dependencies
    # -------------------------------------------------------------------------
    info_log "Installing CRIU runtime dependencies..."
    for c in "${all_containers[@]}"; do
        docker exec "$c" bash -c "
            apt-get update -qq
            apt-get install -y -qq \
                libprotobuf-c1 libnet1 libgnutls30 libnl-3-200 \
                libbsd0 libcap2 python3 python3-pip
        " || {
            error_log "Failed to install dependencies in $c"
            return 1
        }
    done

    # -------------------------------------------------------------------------
    # Copy binaries
    # -------------------------------------------------------------------------
    info_log "Copying cedana + criu binaries into containers..."
    local cedana_bin criu_bin
    cedana_bin=$(command -v cedana 2>/dev/null) ||
        {
            error_log "cedana binary not found in PATH"
            return 1
        }
    criu_bin=$(command -v criu 2>/dev/null) ||
        {
            error_log "criu binary not found in PATH"
            return 1
        }

    for c in "${all_containers[@]}"; do
        docker cp "$cedana_bin" "${c}:/usr/local/bin/cedana" &&
            docker exec "$c" chmod +x /usr/local/bin/cedana ||
            {
                error_log "Failed to install cedana binary in $c"
                return 1
            }
        docker cp "$criu_bin" "${c}:/usr/local/bin/criu" &&
            docker exec "$c" chmod +x /usr/local/bin/criu ||
            {
                error_log "Failed to install criu binary in $c"
                return 1
            }
    done

    # -------------------------------------------------------------------------
    # Copy plugin shared libraries
    # -------------------------------------------------------------------------
    info_log "Copying plugin libraries into containers..."
    for so in /usr/local/lib/libcedana-*.so \
        /usr/local/lib/task_cedana.so \
        /usr/local/lib/libslurm-cedana.so \
        /usr/local/lib/cli_filter_cedana.so; do
        [ -f "$so" ] || continue
        for c in "${all_containers[@]}"; do
            docker cp "$so" "${c}:/usr/local/lib/$(basename "$so")" ||
                {
                    error_log "Failed to copy $(basename "$so") into $c"
                    return 1
                }
        done
    done

    if [ -f "/usr/local/bin/cedana-slurm" ]; then
        for c in "${all_containers[@]}"; do
            docker cp /usr/local/bin/cedana-slurm "${c}:/usr/local/bin/cedana-slurm" &&
                docker exec "$c" chmod +x /usr/local/bin/cedana-slurm ||
                {
                    error_log "Failed to install cedana-slurm in $c"
                    return 1
                }
        done
    fi

    # -------------------------------------------------------------------------
    # Configure SLURM plugins on the controller
    # -------------------------------------------------------------------------
    info_log "Configuring SLURM to load Cedana plugins (controller)..."
    docker exec -i "$SLURM_CONTROLLER_CONTAINER" bash <<'SETUP_EOF' >&3 2>&1 ||
set -euo pipefail

PLUGIN_DIR=$(scontrol show config 2>/dev/null | awk '/^PluginDir/{print $3}')
[ -n "$PLUGIN_DIR" ] || { echo "ERROR: Could not determine SLURM PluginDir" >&2; exit 1; }
echo "SLURM PluginDir: $PLUGIN_DIR"

for f in task_cedana.so cli_filter_cedana.so; do
    src="/usr/local/lib/${f}"
    [ -f "$src" ] || continue
    chmod 755 "$src"
    cp "$src" "$PLUGIN_DIR/"
done
if [ -f /usr/local/lib/libslurm-cedana.so ]; then
    chmod 755 /usr/local/lib/libslurm-cedana.so
    cp /usr/local/lib/libslurm-cedana.so "${PLUGIN_DIR}/spank_cedana.so"
fi
ldconfig

SLURM_CONF="${SLURM_CONF:-/etc/slurm/slurm.conf}"
PLUGSTACK_CONF=$(scontrol show config 2>/dev/null | awk '/^PlugStackConfig/{print $3}')
PLUGSTACK_CONF="${PLUGSTACK_CONF:-/etc/slurm/plugstack.conf}"

grep -q 'task/cedana' "$SLURM_CONF" || \
    sed -i 's|^\(TaskPlugin=.*\)|\1,task/cedana|' "$SLURM_CONF"
grep -q 'cli_filter/cedana' "$SLURM_CONF" || \
    echo 'CliFilterPlugins=cli_filter/cedana' >> "$SLURM_CONF"

if [ -f /usr/local/lib/libslurm-cedana.so ]; then
    grep -q 'spank_cedana.so' "$PLUGSTACK_CONF" 2>/dev/null || \
        echo "required ${PLUGIN_DIR}/spank_cedana.so" >> "$PLUGSTACK_CONF"
fi
SETUP_EOF
        {
            error_log "SLURM plugin setup failed on controller"
            return 1
        }

    # -------------------------------------------------------------------------
    # Install plugins on compute nodes (binaries only — config lives on controller)
    # -------------------------------------------------------------------------
    info_log "Installing Cedana plugins on compute nodes..."
    for c in "${compute_containers[@]}"; do
        docker exec -i "$c" bash <<'COMPUTE_EOF' >&3 2>&1 ||
set -euo pipefail
PLUGIN_DIR=$(scontrol show config 2>/dev/null | awk '/^PluginDir/{print $3}')
[ -n "$PLUGIN_DIR" ] || { echo "ERROR: Could not determine SLURM PluginDir" >&2; exit 1; }
for f in task_cedana.so cli_filter_cedana.so; do
    src="/usr/local/lib/${f}"
    [ -f "$src" ] || continue
    chmod 755 "$src"; cp "$src" "$PLUGIN_DIR/"
done
[ -f /usr/local/lib/libslurm-cedana.so ] && \
    cp /usr/local/lib/libslurm-cedana.so "${PLUGIN_DIR}/spank_cedana.so"
ldconfig
echo "Plugins installed on $(hostname)"
COMPUTE_EOF
            {
                error_log "Plugin setup failed on $c"
                return 1
            }
    done

    # -------------------------------------------------------------------------
    # Start cedana daemon on all nodes
    # -------------------------------------------------------------------------
    info_log "Starting cedana daemon on all nodes..."
    for c in "${all_containers[@]}"; do
        docker exec "$c" mkdir -p /etc/cedana

        # Initialise config
        docker exec \
            -e CEDANA_URL="${CEDANA_URL:-}" \
            -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
            -e CEDANA_ADDRESS="/run/cedana.sock" \
            -e CEDANA_PROTOCOL="unix" \
            -e CEDANA_DB_REMOTE="true" \
            -e CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-info}" \
            -e CEDANA_CHECKPOINT_DIR="${CEDANA_CHECKPOINT_DIR:-cedana://}" \
            "$c" cedana --init-config version ||
            {
                error_log "cedana --init-config failed on $c"
                return 1
            }

        # Launch daemon in the background
        docker exec -d \
            -e CEDANA_URL="${CEDANA_URL:-}" \
            -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
            -e CEDANA_ADDRESS="/run/cedana.sock" \
            -e CEDANA_PROTOCOL="unix" \
            -e CEDANA_DB_REMOTE="true" \
            -e CEDANA_CLIENT_WAIT_FOR_READY="true" \
            -e CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-info}" \
            -e CEDANA_CHECKPOINT_DIR="${CEDANA_CHECKPOINT_DIR:-cedana://}" \
            "$c" bash -c "/usr/local/bin/cedana daemon start --init-config \
                >/var/log/cedana.log 2>&1"
    done

    # Wait for cedana socket on every node
    info_log "Waiting for cedana daemon socket on all nodes..."
    for c in "${all_containers[@]}"; do
        local waited=0
        while [ "$waited" -lt 30 ]; do
            docker exec "$c" test -S /run/cedana.sock 2>/dev/null && break
            sleep 1
            waited=$((waited + 1))
        done
        if [ "$waited" -ge 30 ]; then
            info_log "WARNING: cedana socket not ready on $c after 30s — proceeding"
        else
            info_log "  $c: cedana socket ready (${waited}s)"
        fi
    done

    # -------------------------------------------------------------------------
    # Restart SLURM services to load the new plugins
    # -------------------------------------------------------------------------
    info_log "Restarting SLURM services to load task_cedana plugin..."
    _svc_restart "$SLURM_CONTROLLER_CONTAINER" slurmctld /usr/sbin/slurmctld ||
        {
            error_log "Failed to restart slurmctld"
            return 1
        }
    for c in "${compute_containers[@]}"; do
        _svc_restart "$c" slurmd /usr/sbin/slurmd ||
            {
                error_log "Failed to restart slurmd on $c"
                return 1
            }
    done
    sleep 5

    # -------------------------------------------------------------------------
    # SPANK plugin diagnostics
    # -------------------------------------------------------------------------
    info_log "=== SPANK Plugin Diagnostics ==="
    for c in "${compute_containers[@]}"; do
        info_log "--- $c ---"
        docker exec "$c" bash -c '
            echo "plugstack.conf:"
            cat /etc/slurm/plugstack.conf 2>/dev/null || echo "NOT FOUND"
            echo ""
            echo "spank_cedana.so:"
            ls -la /usr/lib/slurm/spank_cedana.so 2>/dev/null || echo "NOT FOUND"
            echo ""
            ldd /usr/lib/slurm/spank_cedana.so 2>/dev/null || echo "(ldd failed)"
        ' >&3 || true
    done
    info_log "=== End SPANK Plugin Diagnostics ==="

    # -------------------------------------------------------------------------
    # Start cedana-slurm daemon on compute nodes
    # -------------------------------------------------------------------------
    info_log "Starting cedana-slurm daemon on compute nodes..."
    for c in "${compute_containers[@]}"; do
        docker exec -d \
            -e CEDANA_URL="${CEDANA_URL:-}" \
            -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
            -e CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-debug}" \
            "$c" bash -c \
            '/usr/local/bin/cedana-slurm daemon start >/var/log/cedana-slurm.log 2>&1' ||
            {
                error_log "Failed to launch cedana-slurm on $c"
                return 1
            }
    done

    sleep 3
    for c in "${compute_containers[@]}"; do
        if docker exec "$c" pgrep -f 'cedana-slurm daemon' &>/dev/null; then
            info_log "  $c: cedana-slurm daemon running"
        else
            error_log "cedana-slurm daemon failed to start on $c"
            docker exec "$c" tail -20 /var/log/cedana-slurm.log
            return 1
        fi
    done

    wait_for_slurm_ready 180
    info_log "Cedana installed and SLURM cluster is ready"
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
    if docker exec "$SLURM_CONTROLLER_CONTAINER" \
        pgrep -f 'cedana-slurm daemon' &>/dev/null; then
        debug_log "cedana-slurm daemon is running"
    else
        error_log "cedana-slurm daemon failed to start on $SLURM_CONTROLLER_CONTAINER"
        docker exec "$SLURM_CONTROLLER_CONTAINER" tail -20 /var/log/cedana-slurm.log
        return 1
    fi
}

##############################
# Samples Setup
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
            error_log "Failed to clone cedana-samples into $c"
            return 1
        }

        info_log "Verifying cloned structure in $c:"
        docker exec "$c" ls -la /data/cedana-samples/ 2>&1 | head -20 >&3 || true
        docker exec "$c" test -d /data/cedana-samples/slurm/cpu &&
            info_log "  slurm/cpu directory found in $c" ||
            error_log "  slurm/cpu directory NOT found in $c"
    done

    info_log "cedana-samples ready in all cluster nodes"
}

##############################
# Job Management
##############################

# Submit an sbatch job to the SLURM cluster.
# @param $1  Path to sbatch file (any path ending in .../slurm/<subdir>/<file>.sbatch)
# Prints the SLURM job ID on stdout.
slurm_submit_job() {
    local sbatch_file="$1"
    local rel_path container_dir container_file

    rel_path="${sbatch_file#*/slurm/}"
    container_dir="/data/cedana-samples/slurm/$(dirname "$rel_path")"
    container_file="$(basename "$rel_path")"
    debug_log "Submitting: cd $container_dir && sbatch $container_file"

    local output exit_code
    output=$(slurm_exec bash -c \
        "cd '$container_dir' && sbatch --parsable --overcommit \
         --cpus-per-task=1 --mem=0 '$container_file'" 2>&1)
    exit_code=$?

    if [ "$exit_code" -ne 0 ]; then
        error_log "sbatch failed: $output"
        return 1
    fi

    local job_id
    job_id=$(echo "$output" | tail -1 | cut -d';' -f1 | tr -d '[:space:]')
    debug_log "Submitted $container_file -> job $job_id"
    echo "$job_id"
}

# Print detailed failure diagnostics for a job (and recent sacct history).
# @param $1  SLURM job ID (optional)
_dump_job_failure_info() {
    local job_id="${1:-}"

    echo "=== sacct (last 10 jobs) ==="
    slurm_exec sacct --noheader -a \
        --format=JobID,JobName,State,ExitCode,DerivedExitCode,Reason,NodeList,Submit,Start,End \
        -P 2>/dev/null | tail -10 || true

    if [ -n "$job_id" ]; then
        echo "=== scontrol show job $job_id ==="
        slurm_exec scontrol show job "$job_id" 2>/dev/null || true

        echo "=== job output files ==="
        for c in $(_slurm_compute_containers); do
            for f in $(docker exec "$c" \
                find "${SLURM_DATA_DIR}" -name "*-${job_id}.*" 2>/dev/null); do
                echo "--- $c:$f ---"
                docker exec "$c" cat "$f" 2>/dev/null || true
            done
        done
    fi

    echo "=== slurmctld.log (last 50 lines) ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" \
        tail -50 /var/log/slurm/slurmctld.log 2>/dev/null || true

    echo "=== slurmd.log on compute nodes (last 50 lines) ==="
    for c in $(_slurm_compute_containers); do
        echo "--- $c ---"
        docker exec "$c" tail -50 /var/log/slurm/slurmd.log 2>/dev/null ||
            echo "(unavailable)"
    done

    echo "=== cedana daemon log on compute nodes (last 30 lines) ==="
    for c in $(_slurm_compute_containers); do
        echo "--- $c ---"
        docker exec "$c" tail -30 /var/log/cedana.log 2>/dev/null ||
            echo "(no log)"
    done

    echo "=== cedana-slurm log on controller (last 30 lines) ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" \
        tail -30 /var/log/cedana-slurm.log 2>/dev/null || true
}

# Wait for a SLURM job to reach a specific state.
# @param $1  SLURM job ID
# @param $2  Target state (RUNNING, COMPLETED, etc.)
# @param $3  Timeout in seconds (default: 60)
wait_for_slurm_job_state() {
    local job_id="$1"
    local target_state="$2"
    local timeout="${3:-60}"
    local elapsed=0

    while [ "$elapsed" -lt "$timeout" ]; do
        local state
        state=$(slurm_exec scontrol show job "$job_id" 2>/dev/null |
            grep -oP 'JobState=\K\S+' || echo "UNKNOWN")

        debug_log "Job $job_id state: $state (want: $target_state)"

        [ "$state" = "$target_state" ] && return 0

        case "$state" in
        COMPLETED | FAILED | CANCELLED | TIMEOUT | NODE_FAIL)
            error_log "Job $job_id reached terminal state $state (expected $target_state)"
            _dump_job_failure_info "$job_id"
            return 1
            ;;
        esac

        sleep 2
        elapsed=$((elapsed + 2))
    done

    error_log "Timeout: job $job_id did not reach $target_state after ${timeout}s"
    return 1
}

# Return the node a SLURM job is running on.
get_slurm_job_node() {
    slurm_exec scontrol show job "$1" 2>/dev/null |
        grep -oP 'BatchHost=\K\S+' | head -1
}

get_slurm_job_info() {
    slurm_exec scontrol show job "$1" -o 2>/dev/null
}

cancel_slurm_job() {
    slurm_exec scancel "$1" 2>/dev/null || true
}

get_slurm_job_output() {
    local job_id="$1"
    local job_name="${2:-}"
    [ -n "$job_name" ] &&
        docker exec "$SLURM_CONTROLLER_CONTAINER" \
            cat "${SLURM_DATA_DIR}/${job_name}-${job_id}.out" 2>/dev/null || true
}

##############################
# C/R Test Orchestrator
##############################

# Run a checkpoint/restore action sequence against an sbatch job.
# @param $1  Action sequence: SUBMIT_DUMP | SUBMIT_DUMP_RESTORE | SUBMIT_DUMP_RESTORE_DUMP_RESTORE
# @param $2  Path to sbatch file
# @param $3  Seconds to wait after job starts before first dump (default: 10)
# @param $4  Dump/restore action timeout in seconds (default: 120)
test_slurm_job() {
    local action_sequence="$1"
    local sbatch_file="$2"
    local dump_wait_time="${3:-10}"
    local dump_timeout="${4:-120}"

    IFS='_' read -ra actions <<<"$action_sequence"

    local job_id="" action_id="" submitted=false error=""

    for action in "${actions[@]}"; do
        case "$action" in
        SUBMIT)
            [ "$submitted" = true ] && {
                error="Cannot SUBMIT twice"
                break
            }

            debug_log "Submitting job from $sbatch_file..."
            job_id=$(slurm_submit_job "$sbatch_file") ||
                {
                    error="Failed to submit job"
                    break
                }

            wait_for_slurm_job_state "$job_id" "RUNNING" 60 ||
                {
                    error="Job $job_id failed to reach RUNNING"
                    break
                }

            debug_log "Job $job_id running — waiting ${dump_wait_time}s before dump..."
            sleep "$dump_wait_time"
            submitted=true
            ;;

        DUMP)
            [ "$submitted" = false ] && {
                error="Cannot DUMP — no job submitted"
                break
            }
            [ -z "$job_id" ] && {
                error="Cannot DUMP — no active job ID"
                break
            }

            info_log "Checkpointing SLURM job $job_id via propagator..."
            local slurm_job_name checkpoint_output checkpoint_status
            slurm_job_name=$(slurm_exec scontrol show job "$job_id" 2>/dev/null |
                grep -o 'JobName=[^ ]*' | cut -d= -f2)

            checkpoint_output=$(checkpoint_slurm_job "$slurm_job_name")
            checkpoint_status=$?
            [ "$checkpoint_status" -ne 0 ] && {
                error="Checkpoint failed: $checkpoint_output"
                break
            }

            action_id="$checkpoint_output"
            validate_action_id "$action_id" ||
                {
                    error="Invalid action ID: $action_id"
                    break
                }

            poll_slurm_action_status "$action_id" "checkpoint" "$dump_timeout" ||
                {
                    error="Checkpoint action $action_id did not complete"
                    break
                }

            debug_log "Checkpoint complete (action_id: $action_id)"
            ;;

        RESTORE)
            [ -z "$action_id" ] && {
                error="Cannot RESTORE — no checkpoint action ID"
                break
            }

            debug_log "Cancelling job $job_id before restore..."
            cancel_slurm_job "$job_id"
            sleep 2

            debug_log "Restoring job from action $action_id..."
            local restore_output restore_status restore_action_id
            restore_output=$(restore_slurm_job "$action_id" "$SLURM_CLUSTER_ID")
            restore_status=$?
            [ "$restore_status" -ne 0 ] && {
                error="Restore failed: $restore_output"
                break
            }

            restore_action_id="$restore_output"
            validate_action_id "$restore_action_id" ||
                {
                    error="Invalid restore action ID: $restore_action_id"
                    break
                }

            debug_log "Waiting for restored job to appear..."
            sleep 5

            local new_job_id
            new_job_id=$(slurm_exec squeue -h -o '%i' --sort=-V 2>/dev/null | head -1)
            if [ -n "$new_job_id" ] && [ "$new_job_id" != "$job_id" ]; then
                job_id="$new_job_id"
                debug_log "Restored job has new ID: $job_id"
            fi

            wait_for_slurm_job_state "$job_id" "RUNNING" 60 ||
                {
                    error="Restored job $job_id failed to reach RUNNING"
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

    # Always clean up the job
    [ -n "$job_id" ] && cancel_slurm_job "$job_id"

    if [ -n "$error" ]; then
        error_log "$error"
        slurm_exec squeue 2>/dev/null || true
        slurm_exec sinfo 2>/dev/null || true
        _dump_job_failure_info "${job_id:-}"
        return 1
    fi

    return 0
}
