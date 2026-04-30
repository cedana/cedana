#!/usr/bin/env bash
# shellcheck disable=SC2016

##############################
# SLURM Cluster Setup
##############################

CEDANA_SLURM_DIR="${CEDANA_SLURM_DIR:-}"

SLURM_DATA_DIR="${SLURM_DATA_DIR:-/data}"
SLURM_CONTROLLER_CONTAINER="${SLURM_CONTROLLER_CONTAINER:-slurm-controller}"
# Must match docker-deploy.sh COMPUTE_NODES
COMPUTE_NODES="${COMPUTE_NODES:-1}"

##############################
# Internal Helpers
##############################

_slurm_compute_containers() {
    local names=()
    for i in $(seq 1 "$COMPUTE_NODES"); do
        names+=("slurm-compute-$(printf '%02d' "$i")")
    done
    echo "${names[@]}"
}

slurm_exec() {
    docker exec -i "$SLURM_CONTROLLER_CONTAINER" "$@"
}

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

_slurm_conf_set() {
    local container="$1"
    local key="$2"
    local value="$3"
    docker exec "$container" bash -c "
        grep -q '^${key}=' /etc/slurm/slurm.conf 2>/dev/null || \
            echo '${key}=${value}' >> /etc/slurm/slurm.conf
    "
}

_log_gpu_debug_state() {
    local container="$1"
    local phase="${2:-state}"

    [ "${GPU:-0}" = "1" ] || return 0

    info_log "[GPU DEBUG][$phase] $container"
    docker exec "$container" bash -lc '
        echo "=== hostname ==="
        hostname || true
        echo "=== nvidia-smi -L ==="
        nvidia-smi -L 2>&1 || true
        echo "=== /dev/nvidia* ==="
        ls -la /dev/nvidia* 2>&1 || true
        echo "=== /etc/slurm/gres.conf ==="
        cat /etc/slurm/gres.conf 2>&1 || true
        echo "=== /etc/slurm/slurm.conf (GPU lines) ==="
        grep -E "^(NodeName|GresTypes|DebugFlags)" /etc/slurm/slurm.conf 2>&1 || true
        echo "=== slurmd -C ==="
        /usr/sbin/slurmd -C 2>&1 || true
        echo "=== slurmd -G ==="
        /usr/sbin/slurmd -G 2>&1 || true
    ' || true
}

##############################
# Service Management
##############################

_svc_restart() {
    local container="$1"
    local name="$2"
    local binary="$3"
    shift 3
    local extra_args=("$@")
    local proc
    proc=$(basename "$binary")

    debug_log "Restarting $name in $container..."

    docker exec "$container" mkdir -p /run/slurmd /run/slurmctld /run/slurmdbd 2>/dev/null || true

    docker exec "$container" systemctl stop "$name" 2>/dev/null || true
    local waited=0
    while [ "$waited" -lt 10 ]; do
        docker exec "$container" pgrep -x "$proc" &>/dev/null || break
        sleep 1
        waited=$((waited + 1))
    done

    docker exec "$container" systemctl start "$name" 2>/dev/null || true
    sleep 3
    if docker exec "$container" pgrep -x "$proc" &>/dev/null; then
        debug_log "$name started via systemctl in $container"
        return 0
    fi

    docker exec -d \
        -e PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
        "$container" "$binary" "${extra_args[@]}"
    sleep 3
    if docker exec "$container" pgrep -x "$proc" &>/dev/null; then
        debug_log "$name started directly in $container"
        return 0
    fi

    error_log "$name failed to start in $container"
    docker exec "$container" journalctl -u "$name" --no-pager -n 30 2>/dev/null || true
    docker exec "$container" tail -30 /var/log/slurm/${name}.log 2>/dev/null || true
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

    # Use a YAML file for extra vars so booleans stay booleans (not strings).
    # `-e key=value` on the CLI treats the value as string "false", which is
    # truthy in Jinja `when:`.
    local vars_file="/tmp/cedana-slurm-vars-$$.yml"
    cat >"$vars_file" <<'EOF'
slurm_cluster_name: cedana_test_cluster
slurm_accounting_enabled: false
nfs_shared_install: true
EOF

    local rc=0
    pushd "$ansible_dir" >/dev/null
    if ANSIBLE_EXTRA_ARGS="-e @${vars_file}" \
        ANSIBLE_SKIP_TAGS="cedana" bash docker-deploy.sh >&"${OUTPUT_FD}" 2>&1; then
        rc=0
    else
        rc=$?
    fi
    popd >/dev/null
    rm -f "$vars_file"

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

setup_slurm_accounting() {
    info_log "Setting up SLURM accounting (MariaDB + slurmdbd)..."

    local mysql_root_password="${SLURM_MYSQL_ROOT_PASSWORD:-slurmroot123}"
    local slurm_db_name="${SLURM_DB_NAME:-slurm_acct_db}"
    local slurm_db_user="${SLURM_DB_USER:-slurm}"
    local slurm_db_pass="${SLURM_DB_PASSWORD:-slurmdb123}"

    debug_log "[1/7] Installing MariaDB..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        apt-get update -qq
        DEBIAN_FRONTEND=noninteractive apt-get install -y -qq mariadb-server python3-pymysql netcat-openbsd
        mkdir -p /var/run/mysqld
        chown mysql:mysql /var/run/mysqld
    " || {
        error_log "Failed to install MariaDB"
        return 1
    }

    # slurmdbd warns without these innodb settings
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        cat >> /etc/mysql/mariadb.conf.d/50-server.cnf << 'EOF'
innodb_buffer_pool_size = 128M
innodb_lock_wait_timeout = 900
EOF
    " || true # non-fatal; defaults still work

    debug_log "[2/7] Starting MariaDB..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        if [ ! -d /var/lib/mysql/mysql ]; then
            mysql_install_db --user=mysql --basedir=/usr --datadir=/var/lib/mysql 2>/dev/null || true
        fi
        mysqld_safe --bind-address=127.0.0.1 --skip-networking=0 &
        for i in \$(seq 1 30); do
            mysqladmin ping --silent 2>/dev/null && break
            sleep 1
        done
        mysqladmin ping --silent 2>/dev/null || { echo 'MariaDB did not start'; exit 1; }
    " || {
        error_log "Failed to start MariaDB"
        return 1
    }

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

    debug_log "[3/7] Creating SLURM accounting database..."
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

    debug_log "[4/7] Verifying slurmdbd binary..."
    if ! docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "test -x /usr/sbin/slurmdbd" 2>/dev/null; then
        error_log "slurmdbd binary not found at /usr/sbin/slurmdbd"
        return 1
    fi

    local slurmdbd_ver
    slurmdbd_ver=$(docker exec "$SLURM_CONTROLLER_CONTAINER" \
        bash -c "/usr/sbin/slurmdbd -V 2>&1 | head -1 | awk '{print \$NF}'" 2>/dev/null || true)
    debug_log "slurmdbd version: ${slurmdbd_ver:-unknown}"

    debug_log "[5/7] Writing configuration files..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        mkdir -p /etc/slurm /var/log/slurm /var/run/slurmdbd
        cat > /etc/slurm/slurmdbd.conf << EOF
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

    _slurm_conf_set "$SLURM_CONTROLLER_CONTAINER" "AccountingStorageType" "accounting_storage/slurmdbd"
    _slurm_conf_set "$SLURM_CONTROLLER_CONTAINER" "AccountingStorageHost" "localhost"
    _slurm_conf_set "$SLURM_CONTROLLER_CONTAINER" "AccountingStoragePort" "6819"
    _slurm_conf_set "$SLURM_CONTROLLER_CONTAINER" "JobCompType" "jobcomp/none"
    _slurm_conf_set "$SLURM_CONTROLLER_CONTAINER" "JobAcctGatherType" "jobacct_gather/none"
    # Enforcement is added in step 7, after cluster/account/user records exist

    debug_log "[6/7] Starting slurmdbd..."
    docker exec -d "$SLURM_CONTROLLER_CONTAINER" slurmdbd -D
    sleep 5
    _wait_for_port "$SLURM_CONTROLLER_CONTAINER" 6819 60 ||
        {
            error_log "slurmdbd did not open port 6819 in time"
            docker exec "$SLURM_CONTROLLER_CONTAINER" \
                tail -30 /var/log/slurm/slurmdbd.log 2>/dev/null || true
            return 1
        }

    debug_log "[7/7] Starting slurmctld and seeding accounting records..."
    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        mkdir -p /var/spool/slurmctld /var/spool/slurmd
        chown slurm:slurm /var/spool/slurmctld /var/spool/slurmd
    " || true

    docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
        STATE_DIR=\$(awk -F= '/^StateSaveLocation/{gsub(/[[:space:]]/, \"\", \$2); print \$2; exit}' \
            /etc/slurm/slurm.conf 2>/dev/null)
        STATE_DIR=\"\${STATE_DIR:-/var/spool/slurm/ctld}\"
        rm -f \"\${STATE_DIR}/clustername\"
        echo \"Cleared stale ClusterID from \${STATE_DIR}/clustername\"
    " || true

    _svc_restart "$SLURM_CONTROLLER_CONTAINER" slurmctld /usr/sbin/slurmctld ||
        {
            error_log "Failed to start slurmctld"
            return 1
        }
    sleep 5

    debug_log "Waiting for sacctmgr to reach slurmdbd (up to 90s)..."
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

    slurm_exec sacctmgr -i add cluster cluster 2>/dev/null || true
    slurm_exec sacctmgr -i add account default \
        Description="Default Account" Organization="default" 2>/dev/null || true
    slurm_exec sacctmgr -i add user root \
        Account=default AdminLevel=Admin 2>/dev/null || true

    _slurm_conf_set "$SLURM_CONTROLLER_CONTAINER" \
        "AccountingStorageEnforce" "associations,limits,qos"

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
        if ! node_state=$(slurm_exec sinfo -h -o '%T' 2>/dev/null | head -1); then
            node_state=""
        fi
        debug_log "  [${elapsed}s] node state: ${node_state:-<no response>}"

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
    if [ "${GPU:-0}" = "1" ]; then
        for c in $(_slurm_compute_containers); do
            _log_gpu_debug_state "$c" "slurm-not-ready"
        done
    fi
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

install_cedana_in_slurm() {
    info_log "Installing Cedana into SLURM cluster containers..."

    local all_containers=("$SLURM_CONTROLLER_CONTAINER")
    local compute_containers=()
    # shellcheck disable=SC2207
    compute_containers=($(_slurm_compute_containers))
    all_containers+=("${compute_containers[@]}")

    debug_log "Installing CRIU runtime dependencies..."
    for c in "${all_containers[@]}"; do
        docker exec "$c" bash -c "
            apt-get update -qq
            apt-get install -y -qq \
                libprotobuf-c1 libnet1 libgnutls30 libnl-3-200 \
                libbsd0 libcap2 libcap2-bin libnftables1 iptables \
                python3 python3-pip python3-venv
        " || {
            error_log "Failed to install dependencies in $c"
            return 1
        }
    done

    debug_log "Copying cedana + criu binaries into containers..."
    local cedana_bin criu_bin
    cedana_bin="${CEDANA_BIN:-}"
    if [ -z "$cedana_bin" ]; then
        cedana_bin=$(command -v cedana 2>/dev/null) ||
            {
                error_log "cedana binary not found in PATH"
                return 1
            }
    fi
    if [ ! -x "$cedana_bin" ]; then
        error_log "cedana binary not executable at $cedana_bin"
        return 1
    fi
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

    debug_log "Copying plugin libraries into containers..."
    for so in /usr/local/lib/libcedana-*.so \
        /usr/local/lib/task_cedana.so \
        /usr/local/lib/spank_cedana.so \
        /usr/local/lib/cli_filter_cedana.so \
        /usr/local/lib/job_submit_cedana.so; do
        [ -f "$so" ] || continue
        for c in "${all_containers[@]}"; do
            docker cp "$so" "${c}:/usr/local/lib/$(basename "$so")" ||
                {
                    error_log "Failed to copy $(basename "$so") into $c"
                    return 1
                }
        done
    done

    if [ ! -f "/usr/local/bin/cedana-slurm" ]; then
        error_log "cedana-slurm binary not found at /usr/local/bin/cedana-slurm"
        return 1
    fi

    for c in "${all_containers[@]}"; do
        docker cp /usr/local/bin/cedana-slurm "${c}:/usr/local/bin/cedana-slurm" &&
            docker exec "$c" bash -c '
                set -euo pipefail
                chmod +x /usr/local/bin/cedana-slurm
                install -m 0755 /usr/local/bin/cedana-slurm /usr/bin/cedana-slurm
                install -m 0755 /usr/local/bin/cedana-slurm /usr/sbin/cedana-slurm
            ' ||
            {
                error_log "Failed to install cedana-slurm in $c"
                return 1
            }

        docker exec "$c" bash -c 'test -x /usr/local/bin/cedana-slurm && test -x /usr/bin/cedana-slurm && /usr/local/bin/cedana-slurm --help >/dev/null' ||
            {
                error_log "cedana-slurm binary verification failed in $c"
                return 1
            }

        docker exec "$c" bash -c '
            grep -q "^PATH=" /etc/default/slurmd 2>/dev/null &&
                sed -i "s|^PATH=.*|PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin|" /etc/default/slurmd ||
                echo "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" >> /etc/default/slurmd
            grep -q "^CEDANA_SLURM_BIN=" /etc/default/slurmd 2>/dev/null &&
                sed -i "s|^CEDANA_SLURM_BIN=.*|CEDANA_SLURM_BIN=/usr/bin/cedana-slurm|" /etc/default/slurmd ||
                echo "CEDANA_SLURM_BIN=/usr/bin/cedana-slurm" >> /etc/default/slurmd
            printf "export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n" > /etc/profile.d/cedana-slurm-path.sh
            chmod 644 /etc/profile.d/cedana-slurm-path.sh
            mkdir -p /etc/systemd/system/slurmd.service.d /etc/systemd/system/slurmctld.service.d
            cat >/etc/systemd/system/slurmd.service.d/cedana-path.conf <<"EOF"
[Service]
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
Environment=CEDANA_SLURM_BIN=/usr/bin/cedana-slurm
EOF
            cat >/etc/systemd/system/slurmctld.service.d/cedana-path.conf <<"EOF"
[Service]
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
Environment=CEDANA_SLURM_BIN=/usr/bin/cedana-slurm
EOF

            if command -v systemctl >/dev/null 2>&1; then
                systemctl daemon-reload >/dev/null 2>&1 || true
            fi
        ' || {
            error_log "Failed to enforce PATH for slurmd in $c"
            return 1
        }
    done

    local storage_plugin=""
    case "${CEDANA_CHECKPOINT_DIR:-}" in
        cedana://*) storage_plugin="storage/cedana" ;;
        s3://*) storage_plugin="storage/s3" ;;
        gcs://*) storage_plugin="storage/gcs" ;;
    esac

    local runtime_plugins="runc"
    if [ -n "$storage_plugin" ]; then
        runtime_plugins="$runtime_plugins $storage_plugin"
    fi

    debug_log "Installing SLURM plugin via Cedana CLI in all nodes..."
    for c in "${all_containers[@]}"; do
        local node_role="worker"
        if [ "$c" = "$SLURM_CONTROLLER_CONTAINER" ]; then
            node_role="controller"
        fi

        docker exec \
            -e CEDANA_PLUGINS_BUILDS="local" \
            -e CEDANA_PLUGINS_LOCAL_SEARCH_PATH="/usr/local/lib:/usr/local/bin" \
            -e CEDANA_PLUGINS_LIB_DIR="/usr/local/lib" \
            -e CEDANA_PLUGINS_BIN_DIR="/usr/local/bin" \
            -e CEDANA_SLURM_NODE_ROLE="$node_role" \
            "$c" bash -c '
                set -euo pipefail
                cedana plugin install slurm
                cedana slurm setup --node-role "$CEDANA_SLURM_NODE_ROLE"
            ' >&"${OUTPUT_FD}" 2>&1 ||
            {
                error_log "Cedana SLURM plugin install/setup failed on $c"
                return 1
            }

        docker exec \
            -e CEDANA_URL="${CEDANA_URL:-}" \
            -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
            -e CEDANA_PLUGINS_BUILDS="${CEDANA_PLUGINS_BUILDS:-release}" \
            -e CEDANA_PLUGINS_LOCAL_SEARCH_PATH="/usr/local/lib:/usr/local/bin" \
            -e CEDANA_PLUGINS_LIB_DIR="/usr/local/lib" \
            -e CEDANA_PLUGINS_BIN_DIR="/usr/local/bin" \
            "$c" bash -c "
                set -euo pipefail
                cedana plugin install ${runtime_plugins}
            " >&"${OUTPUT_FD}" 2>&1 ||
            {
                error_log "Cedana runtime plugin install failed on $c"
                return 1
            }
    done

    if [ "${GPU:-0}" = "1" ]; then
        debug_log "Configuring SLURM GPU GRES resources..."
        for c in "${compute_containers[@]}"; do
            local gpu_count
            local detected_gres
            gpu_count=$(docker exec "$c" bash -c 'ls -1 /dev/nvidia[0-9]* 2>/dev/null | wc -l' || echo "0")
            if [ "$gpu_count" -eq 0 ]; then
                error_log "GPU test requested but no /dev/nvidia* devices were found in $c"
                docker exec "$c" bash -c 'ls -la /dev/nvidia* 2>/dev/null || true; nvidia-smi -L 2>/dev/null || true' || true
                return 1
            fi
            debug_log "Detected $gpu_count GPU(s) on $c"

            local node_hostname
            node_hostname=$(docker exec "$c" hostname)

            if ! detected_gres=$(docker exec "$c" bash -lc "/usr/sbin/slurmd -C 2>/dev/null | tr ' ' '\n' | grep '^Gres=' | cut -d= -f2- | head -n 1"); then
                detected_gres=""
            fi
            if [ -z "$detected_gres" ]; then
                detected_gres="gpu:$gpu_count"
                debug_log "slurmd -C did not report GRES on $c, falling back to $detected_gres"
            else
                debug_log "slurmd -C detected GRES '$detected_gres' on $c"
            fi

            docker exec "$c" bash -c "
                mkdir -p /etc/slurm
                echo 'AutoDetect=nvidia' > /etc/slurm/gres.conf
                cat /etc/slurm/gres.conf
            " || {
                error_log "Failed to write autodetect gres.conf on $c"
                return 1
            }

            _log_gpu_debug_state "$c" "post-gres-config"

            docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
                set -euo pipefail
                SLURM_CONF=\"\${SLURM_CONF:-/etc/slurm/slurm.conf}\"

                grep -q '^GresTypes=' \"\$SLURM_CONF\" || \
                    echo 'GresTypes=gpu' >> \"\$SLURM_CONF\"

                if grep -q '^NodeName=$node_hostname' \"\$SLURM_CONF\"; then
                    if ! grep '^NodeName=$node_hostname' \"\$SLURM_CONF\" | grep -q 'Gres='; then
                        sed -i 's|^\(NodeName=$node_hostname.*\)|\1 Gres=gpu:$gpu_count|' \"\$SLURM_CONF\"
                    fi
                fi
                echo 'GRES config updated for $node_hostname'
            " || {
                error_log "Failed to update slurm.conf GRES on controller for $c"
                return 1
            }

            docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
                set -euo pipefail
                SLURM_CONF=\"\${SLURM_CONF:-/etc/slurm/slurm.conf}\"

                grep -q '^DebugFlags=.*NO_CONF_HASH' \"\$SLURM_CONF\" || \
                    echo 'DebugFlags=NO_CONF_HASH' >> \"\$SLURM_CONF\"

                if grep -q '^NodeName=$node_hostname' \"\$SLURM_CONF\"; then
                    if grep '^NodeName=$node_hostname' \"\$SLURM_CONF\" | grep -q 'Gres='; then
                        sed -i 's|^\(NodeName=$node_hostname.*\) Gres=[^[:space:]]*|\1 Gres=$detected_gres|' \"\$SLURM_CONF\"
                    else
                        sed -i 's|^\(NodeName=$node_hostname.*\)|\1 Gres=$detected_gres|' \"\$SLURM_CONF\"
                    fi
                fi
            " || {
                error_log "Failed to align controller GRES with detected value '$detected_gres' for $c"
                return 1
            }
        done

        # SLURM requires gres.conf on all nodes
        docker exec "$SLURM_CONTROLLER_CONTAINER" bash -c "
            mkdir -p /etc/slurm
            test -f /etc/slurm/gres.conf || echo '' > /etc/slurm/gres.conf
        "

        # slurm.conf must be identical across all nodes
        for c in "${compute_containers[@]}"; do
            docker cp "$SLURM_CONTROLLER_CONTAINER:/etc/slurm/slurm.conf" "/tmp/slurm.conf.sync.$$"
            docker cp "/tmp/slurm.conf.sync.$$" "$c:/etc/slurm/slurm.conf"
            debug_log "Synced slurm.conf to $c"
            _log_gpu_debug_state "$c" "post-slurm-conf-sync"
        done
        rm -f /tmp/slurm.conf.sync.$$
    fi

    debug_log "Configuring SLURM to load Cedana plugins (controller)..."
    docker exec -i "$SLURM_CONTROLLER_CONTAINER" bash <<'SETUP_EOF' >&"${OUTPUT_FD}" 2>&1 ||
set -euo pipefail

SLURM_CONF="${SLURM_CONF:-/etc/slurm/slurm.conf}"
PLUGIN_DIR=$(awk -F= '/^PluginDir/{print $2; exit}' "$SLURM_CONF" 2>/dev/null || true)
if [ -z "$PLUGIN_DIR" ]; then
    PLUGIN_DIR=$(scontrol show config 2>/dev/null | awk '/^PluginDir[[:space:]]*=/{print $NF}' || true)
fi
PLUGIN_DIR="${PLUGIN_DIR:-/usr/lib/slurm}"
echo "SLURM PluginDir: $PLUGIN_DIR"
mkdir -p "$PLUGIN_DIR"

for f in task_cedana.so cli_filter_cedana.so job_submit_cedana.so; do
    src="/usr/local/lib/${f}"
    [ -f "$src" ] || continue
    chmod 755 "$src"
    cp "$src" "$PLUGIN_DIR/"
done
if [ -f /usr/local/lib/spank_cedana.so ]; then
    chmod 755 /usr/local/lib/spank_cedana.so
    cp /usr/local/lib/spank_cedana.so "${PLUGIN_DIR}/spank_cedana.so"
fi
ldconfig

PLUGSTACK_CONF=$(scontrol show config 2>/dev/null | awk '/^PlugStackConfig/{print $3}' || true)
PLUGSTACK_CONF="${PLUGSTACK_CONF:-/etc/slurm/plugstack.conf}"

grep -q 'task/cedana' "$SLURM_CONF" || \
    sed -i 's|^\(TaskPlugin=.*\)|\1,task/cedana|' "$SLURM_CONF"
    grep -q 'cli_filter/cedana' "$SLURM_CONF" || \
    echo 'CliFilterPlugins=cli_filter/cedana' >> "$SLURM_CONF"
grep -q 'NO_CONF_HASH' "$SLURM_CONF" || \
    echo 'DebugFlags=NO_CONF_HASH' >> "$SLURM_CONF"
if [ -f "${PLUGIN_DIR}/job_submit_cedana.so" ]; then
    grep -q 'job_submit/cedana' "$SLURM_CONF" || \
        echo 'JobSubmitPlugins=job_submit/cedana' >> "$SLURM_CONF"
fi

if [ -f /usr/local/lib/spank_cedana.so ]; then
    grep -q 'spank_cedana.so' "$PLUGSTACK_CONF" 2>/dev/null || \
        echo "required ${PLUGIN_DIR}/spank_cedana.so" >> "$PLUGSTACK_CONF"
fi
SETUP_EOF
        {
            error_log "SLURM plugin setup failed on controller"
            return 1
        }

    debug_log "Installing Cedana plugins on compute nodes..."
    for c in "${compute_containers[@]}"; do
        docker exec -i "$c" bash <<'COMPUTE_EOF' >&"${OUTPUT_FD}" 2>&1 ||
set -euo pipefail
SLURM_CONF="${SLURM_CONF:-/etc/slurm/slurm.conf}"
PLUGIN_DIR=$(awk -F= '/^PluginDir/{print $2; exit}' "$SLURM_CONF" 2>/dev/null || true)
if [ -z "$PLUGIN_DIR" ]; then
    PLUGIN_DIR=$(scontrol show config 2>/dev/null | awk '/^PluginDir[[:space:]]*=/{print $NF}' || true)
fi
PLUGIN_DIR="${PLUGIN_DIR:-/usr/lib/slurm}"
mkdir -p "$PLUGIN_DIR"
for f in task_cedana.so cli_filter_cedana.so job_submit_cedana.so; do
    src="/usr/local/lib/${f}"
    [ -f "$src" ] || continue
    chmod 755 "$src"; cp "$src" "$PLUGIN_DIR/"
done
[ -f /usr/local/lib/spank_cedana.so ] && \
    cp /usr/local/lib/spank_cedana.so "${PLUGIN_DIR}/spank_cedana.so"
ldconfig

grep -q 'task/cedana' "$SLURM_CONF" || \
    sed -i 's|^\(TaskPlugin=.*\)|\1,task/cedana|' "$SLURM_CONF"
grep -q 'cli_filter/cedana' "$SLURM_CONF" || \
    echo 'CliFilterPlugins=cli_filter/cedana' >> "$SLURM_CONF"

# Suppress conf-hash mismatch from plugin entries
grep -q 'NO_CONF_HASH' "$SLURM_CONF" || \
    echo 'DebugFlags=NO_CONF_HASH' >> "$SLURM_CONF"

PLUGSTACK_CONF=$(scontrol show config 2>/dev/null | awk '/^PlugStackConfig/{print $3}' || true)
PLUGSTACK_CONF="${PLUGSTACK_CONF:-/etc/slurm/plugstack.conf}"
if [ -f /usr/local/lib/spank_cedana.so ]; then
    grep -q 'spank_cedana.so' "$PLUGSTACK_CONF" 2>/dev/null || \
        echo "required ${PLUGIN_DIR}/spank_cedana.so" >> "$PLUGSTACK_CONF"
fi

echo "Plugins and config updated on $(hostname)"
COMPUTE_EOF
            {
                error_log "Plugin setup failed on $c"
                return 1
            }
    done

    debug_log "Re-syncing final slurm.conf to compute nodes..."
    for c in "${compute_containers[@]}"; do
        docker cp "$SLURM_CONTROLLER_CONTAINER:/etc/slurm/slurm.conf" "/tmp/slurm.conf.final.sync.$$"
        docker cp "/tmp/slurm.conf.final.sync.$$" "$c:/etc/slurm/slurm.conf"
        debug_log "Synced final slurm.conf to $c"
    done
    rm -f /tmp/slurm.conf.final.sync.$$

    debug_log "Starting cedana daemon on all nodes..."
    for c in "${all_containers[@]}"; do
        docker exec "$c" mkdir -p /etc/cedana

        docker exec "$c" bash -c "
            pkill -f 'cedana daemon' 2>/dev/null || true
            sleep 1
            rm -f /run/cedana.sock
        " || true

        docker exec \
            -e CEDANA_URL="${CEDANA_URL:-}" \
            -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
            -e CEDANA_ADDRESS="/run/cedana.sock" \
            -e CEDANA_PROTOCOL="unix" \
            -e CEDANA_DB_REMOTE="true" \
            -e CEDANA_BIN="${CEDANA_BIN:-/usr/local/bin/cedana}" \
            -e CEDANA_PLUGINS_LIB_DIR="/usr/local/lib" \
            -e CEDANA_PLUGINS_BIN_DIR="/usr/local/bin" \
            -e CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-info}" \
            -e CEDANA_CHECKPOINT_DIR="${CEDANA_CHECKPOINT_DIR:-cedana://}" \
            "$c" "$cedana_bin" --merge-config version ||
            {
                error_log "cedana --merge-config failed on $c"
                return 1
            }

        docker exec -d \
            -e CEDANA_URL="${CEDANA_URL:-}" \
            -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
            -e CEDANA_ADDRESS="/run/cedana.sock" \
            -e CEDANA_PROTOCOL="unix" \
            -e CEDANA_DB_REMOTE="true" \
            -e CEDANA_CLIENT_WAIT_FOR_READY="true" \
            -e CEDANA_BIN="${CEDANA_BIN:-/usr/local/bin/cedana}" \
            -e CEDANA_PLUGINS_LIB_DIR="/usr/local/lib" \
            -e CEDANA_PLUGINS_BIN_DIR="/usr/local/bin" \
            -e CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-info}" \
            -e CEDANA_CHECKPOINT_DIR="${CEDANA_CHECKPOINT_DIR:-cedana://}" \
            "$c" bash -c "\"${CEDANA_BIN:-/usr/local/bin/cedana}\" daemon start \
                >/var/log/cedana.log 2>&1"
    done

    debug_log "Waiting for cedana daemon socket on all nodes..."
    for c in "${all_containers[@]}"; do
        local waited=0
        while [ "$waited" -lt 30 ]; do
            docker exec "$c" test -S /run/cedana.sock 2>/dev/null && break
            sleep 1
            waited=$((waited + 1))
        done
        if [ "$waited" -ge 30 ]; then
            error_log "cedana socket not ready on $c after 30s"
            docker exec "$c" tail -20 /var/log/cedana.log 2>/dev/null || true
            return 1
        fi
        debug_log "  $c: cedana socket ready (${waited}s)"
    done

    debug_log "Restarting SLURM services to load task_cedana plugin..."
    _svc_restart "$SLURM_CONTROLLER_CONTAINER" slurmctld /usr/sbin/slurmctld ||
        {
            error_log "Failed to restart slurmctld"
            return 1
        }
    _svc_restart "$SLURM_CONTROLLER_CONTAINER" slurmd /usr/sbin/slurmd ||
        {
            error_log "Failed to restart controller slurmd"
            return 1
        }
    _log_gpu_debug_state "$SLURM_CONTROLLER_CONTAINER" "post-controller-slurmd-restart"
    for c in "${compute_containers[@]}"; do
        _svc_restart "$c" slurmd /usr/sbin/slurmd ||
            {
                error_log "Failed to restart slurmd on $c"
                return 1
            }
        _log_gpu_debug_state "$c" "post-slurmd-restart"
    done
    sleep 5

    if [ "${GPU:-0}" = "1" ]; then
        debug_log "Clearing transient GPU drain state after SLURM restarts..."
        for c in "${compute_containers[@]}"; do
            local node_hostname
            node_hostname=$(docker exec "$c" hostname)
            # slurmctld may briefly drain the node while it still sees the
            # pre-restart registration without GRES; clear that once slurmd is back.
            slurm_exec scontrol update NodeName="$node_hostname" State=RESUME \
                >/dev/null 2>&1 || true
        done
    fi

    debug_log "Restarting cedana daemon on all nodes (post-SLURM restart)..."
    for c in "${all_containers[@]}"; do
        docker exec "$c" bash -c "
            pkill -f 'cedana daemon' 2>/dev/null || true
            sleep 1
            rm -f /run/cedana.sock
        " || true

        docker exec -d \
            -e CEDANA_URL="${CEDANA_URL:-}" \
            -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
            -e CEDANA_ADDRESS="/run/cedana.sock" \
            -e CEDANA_PROTOCOL="unix" \
            -e CEDANA_DB_REMOTE="true" \
            -e CEDANA_CLIENT_WAIT_FOR_READY="true" \
            -e CEDANA_BIN="${CEDANA_BIN:-/usr/local/bin/cedana}" \
            -e CEDANA_PLUGINS_LIB_DIR="/usr/local/lib" \
            -e CEDANA_PLUGINS_BIN_DIR="/usr/local/bin" \
            -e CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-info}" \
            -e CEDANA_CHECKPOINT_DIR="${CEDANA_CHECKPOINT_DIR:-cedana://}" \
            "$c" bash -c "\"${CEDANA_BIN:-/usr/local/bin/cedana}\" daemon start \
                >/var/log/cedana.log 2>&1"
    done

    debug_log "Waiting for cedana daemon socket on all nodes (post-SLURM)..."
    for c in "${all_containers[@]}"; do
        local waited=0
        while [ "$waited" -lt 30 ]; do
            docker exec "$c" test -S /run/cedana.sock 2>/dev/null && break
            sleep 1
            waited=$((waited + 1))
        done
        if [ "$waited" -ge 30 ]; then
            error_log "cedana socket not ready on $c after 30s (post-restart)"
            docker exec "$c" tail -20 /var/log/cedana.log 2>/dev/null || true
            return 1
        fi
        debug_log "  $c: cedana socket ready post-restart (${waited}s)"
    done

    local cluster_id="${CEDANA_CLUSTER_ID:-${SLURM_CLUSTER_ID:-}}"
    cluster_id="${cluster_id//\"/}"

    if [ -n "$cluster_id" ]; then
        debug_log "Starting cedana-slurm daemon on compute nodes..."
        for c in "${compute_containers[@]}"; do
            docker exec "$c" bash -c "pkill -x cedana-slurm 2>/dev/null || true"
            docker exec -d \
                -e CEDANA_URL="${CEDANA_URL:-}" \
                -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
                -e CEDANA_CLUSTER_ID="$cluster_id" \
                -e CEDANA_SLURM_BIN="${CEDANA_SLURM_BIN:-/usr/bin/cedana-slurm}" \
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
                debug_log "  $c: cedana-slurm daemon running"
            else
                error_log "cedana-slurm daemon failed to start on $c"
                docker exec "$c" tail -20 /var/log/cedana-slurm.log 2>/dev/null || true
                return 1
            fi
        done
    else
        debug_log "Skipping cedana-slurm daemon startup on compute nodes: cluster ID not set yet"
    fi

    wait_for_slurm_ready 180
    info_log "Cedana installed and SLURM cluster is ready"
}

start_cedana_slurm_daemon() {
    local cluster_id="${CEDANA_CLUSTER_ID:-${SLURM_CLUSTER_ID:-}}"
    cluster_id="${cluster_id//\"/}"

    if [ -z "$cluster_id" ]; then
        error_log "SLURM cluster ID is required to start cedana-slurm daemon"
        error_log "Set SLURM_CLUSTER_ID or CEDANA_CLUSTER_ID before calling start_cedana_slurm_daemon"
        return 1
    fi

    debug_log "Starting cedana-slurm daemon on SLURM nodes..."

    local targets=("$SLURM_CONTROLLER_CONTAINER")
    local compute_containers=($(_slurm_compute_containers))
    targets+=("${compute_containers[@]}")

    if [ -n "${CEDANA_SLURM_BIN:-}" ] && [ -f "$CEDANA_SLURM_BIN" ]; then
        for c in "${targets[@]}"; do
            docker cp "$CEDANA_SLURM_BIN" "${c}:/usr/local/bin/cedana-slurm" ||
                {
                    error_log "Failed to copy CEDANA_SLURM_BIN into $c"
                    return 1
                }
        done
    fi

    for c in "${targets[@]}"; do
        docker exec "$c" bash -c '
            set -euo pipefail
            test -x /usr/local/bin/cedana-slurm
            install -m 0755 /usr/local/bin/cedana-slurm /usr/bin/cedana-slurm
            install -m 0755 /usr/local/bin/cedana-slurm /usr/sbin/cedana-slurm
            test -x /usr/bin/cedana-slurm
            test -x /usr/sbin/cedana-slurm
        ' ||
            {
                error_log "cedana-slurm binary install/verification failed in $c"
                return 1
            }
    done

    for c in "${targets[@]}"; do
        docker exec "$c" bash -c "pkill -x cedana-slurm 2>/dev/null || true"
        docker exec -d \
            -e CEDANA_URL="${CEDANA_URL:-}" \
            -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
            -e CEDANA_CLUSTER_ID="$cluster_id" \
            -e CEDANA_SLURM_BIN="${CEDANA_SLURM_BIN:-/usr/bin/cedana-slurm}" \
            -e CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-debug}" \
            "$c" \
            bash -c '/usr/local/bin/cedana-slurm daemon start >/var/log/cedana-slurm.log 2>&1' ||
            {
                error_log "Failed to launch cedana-slurm daemon on $c"
                return 1
            }
    done

    sleep 3
    for c in "${targets[@]}"; do
        if docker exec "$c" pgrep -f 'cedana-slurm daemon' &>/dev/null; then
            debug_log "  $c: cedana-slurm daemon running"
        else
            error_log "cedana-slurm daemon failed to start on $c"
            docker exec "$c" tail -20 /var/log/cedana-slurm.log 2>/dev/null || true
            return 1
        fi
    done
}

# Restart the cedana-slurm daemon with CEDANA_SLURM_UNPRIVILEGED=1 on all nodes.
# Sets CAP_SYS_PTRACE,CAP_DAC_READ_SEARCH,CAP_CHECKPOINT_RESTORE on the binary via setcap,
# then starts the daemon with CEDANA_SLURM_UNPRIVILEGED=1 so both the daemon and any monitors
# it spawns use the embedded dump path.
restart_cedana_slurm_daemon_unprivileged() {
    local cluster_id="${CEDANA_CLUSTER_ID:-${SLURM_CLUSTER_ID:-}}"
    cluster_id="${cluster_id//\"/}"

    if [ -z "$cluster_id" ]; then
        error_log "SLURM cluster ID is required"
        return 1
    fi

    local targets=("$SLURM_CONTROLLER_CONTAINER")
    local compute_containers=($(_slurm_compute_containers))
    targets+=("${compute_containers[@]}")

    debug_log "Restarting cedana-slurm daemon (unprivileged/embedded) on SLURM nodes..."

    for c in "${targets[@]}"; do
        docker exec "$c" bash -c "pkill -x cedana-slurm 2>/dev/null || true"
        docker exec "$c" bash -c '
            setcap cap_dac_read_search,cap_sys_ptrace,cap_checkpoint_restore=eip /usr/local/bin/cedana-slurm
            setcap cap_dac_read_search,cap_sys_ptrace,cap_checkpoint_restore=eip /usr/bin/cedana-slurm 2>/dev/null || true
        ' || {
            error_log "Failed to set capabilities on cedana-slurm in $c"
            return 1
        }
        docker exec -d \
            -e CEDANA_URL="${CEDANA_URL:-}" \
            -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
            -e CEDANA_CLUSTER_ID="$cluster_id" \
            -e CEDANA_SLURM_BIN="${CEDANA_SLURM_BIN:-/usr/bin/cedana-slurm}" \
            -e CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-debug}" \
            -e CEDANA_SLURM_UNPRIVILEGED=1 \
            "$c" \
            bash -c '/usr/local/bin/cedana-slurm daemon start >/var/log/cedana-slurm.log 2>&1' || {
            error_log "Failed to launch cedana-slurm daemon (unprivileged) on $c"
            return 1
        }
    done

    sleep 3
    for c in "${targets[@]}"; do
        if docker exec "$c" pgrep -f 'cedana-slurm daemon' &>/dev/null; then
            debug_log "  $c: cedana-slurm daemon (unprivileged) running"
        else
            error_log "cedana-slurm daemon (unprivileged) failed to start on $c"
            docker exec "$c" tail -20 /var/log/cedana-slurm.log 2>/dev/null || true
            return 1
        fi
    done
}

# Restart the cedana-slurm daemon in privileged mode (using cedana client) on all nodes.
restart_cedana_slurm_daemon() {
    local cluster_id="${CEDANA_CLUSTER_ID:-${SLURM_CLUSTER_ID:-}}"
    cluster_id="${cluster_id//\"/}"

    if [ -z "$cluster_id" ]; then
        error_log "SLURM cluster ID is required"
        return 1
    fi

    local targets=("$SLURM_CONTROLLER_CONTAINER")
    local compute_containers=($(_slurm_compute_containers))
    targets+=("${compute_containers[@]}")

    debug_log "Restarting cedana-slurm daemon (privileged) on SLURM nodes..."

    for c in "${targets[@]}"; do
        docker exec "$c" bash -c "pkill -x cedana-slurm 2>/dev/null || true"
        docker exec -d \
            -e CEDANA_URL="${CEDANA_URL:-}" \
            -e CEDANA_AUTH_TOKEN="${CEDANA_AUTH_TOKEN:-}" \
            -e CEDANA_CLUSTER_ID="$cluster_id" \
            -e CEDANA_SLURM_BIN="${CEDANA_SLURM_BIN:-/usr/bin/cedana-slurm}" \
            -e CEDANA_LOG_LEVEL="${CEDANA_LOG_LEVEL:-debug}" \
            "$c" \
            bash -c '/usr/local/bin/cedana-slurm daemon start >/var/log/cedana-slurm.log 2>&1' || {
            error_log "Failed to launch cedana-slurm daemon on $c"
            return 1
        }
    done

    sleep 3
    for c in "${targets[@]}"; do
        if docker exec "$c" pgrep -f 'cedana-slurm daemon' &>/dev/null; then
            debug_log "  $c: cedana-slurm daemon running"
        else
            error_log "cedana-slurm daemon failed to start on $c"
            docker exec "$c" tail -20 /var/log/cedana-slurm.log 2>/dev/null || true
            return 1
        fi
    done
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

        docker exec "$c" ls -la /data/cedana-samples/ 2>&1 | head -20 >&"${OUTPUT_FD}" || true
    done

    debug_log "Initializing Python virtual environment and patching sbatch files..."
    for c in "$SLURM_CONTROLLER_CONTAINER" $(_slurm_compute_containers); do
        docker exec "$c" bash -c "
            python3 -m venv /data/venv
            /data/venv/bin/pip install --upgrade pip
        "
        docker exec "$c" bash -c '
            find /data/cedana-samples/slurm -name "*.sbatch" -type f -exec sed -i "s|^#!/bin/bash|#!/bin/bash\nsource /data/venv/bin/activate|" {} +
        '
    done

    info_log "cedana-samples ready in all cluster nodes"
}
