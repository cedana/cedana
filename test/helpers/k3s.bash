#!/usr/bin/env bash

export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
INSTALL_K3S_EXEC="server \
        --write-kubeconfig-mode=644 \
        --disable=traefik \
        --snapshotter=native"

##################################
### K3s and Helm Setup Helpers ###
##################################

kubectl() {
    command k3s kubectl "$@"
}

# Function to set up k3s cluster
setup_k3s_cluster() {
    debug_log "Installing k3s cluster..."

    wget https://get.k3s.io -O /tmp/k3s-install.sh
    chmod +x /tmp/k3s-install.sh

    if ! /tmp/k3s-install.sh; then
        debug_log "Installer failed, will try the binary directly"
        debug_log "Starting k3s cluster setup..."
        start_k3s_cluster
    fi

    debug_log "k3s cluster is ready"
}

start_k3s_cluster() {
    debug_log "Starting k3s cluster..."

    if ! command -v k3s &> /dev/null; then
        debug_log "k3s binary not found"
        return 1
    else
        debug_log "k3s binary found, starting k3s..."
        k3s $INSTALL_K3S_EXEC &
    fi

    debug_log "Waiting for k3s cluster to start..."
    local seconds=0
    local timeout=60
    until [ "$(kubectl get nodes --no-headers 2>/dev/null | wc -l)" -ge 1 ]; do
        (( seconds >= timeout )) && {
            debug_log "Timed out waiting for k3s node object to exist"
            return 1
        }
        sleep 1
    done

    debug_log "Waiting for k3s node to be Ready..."
    if ! kubectl wait --for=condition=Ready node --all --timeout=60s; then
        debug_log "Timed out waiting for k3s node to be Ready"
        return 1
    fi

    debug_log "k3s cluster has started successfully"
}

stop_k3s_cluster() {
    debug_log "Stopping k3s cluster..."

    if command -v k3s-killall.sh &> /dev/null; then
        debug_log "Running k3s killall script..."
        timeout 60 k3s-killall.sh || echo "k3s killall script timed out or failed"
    fi

    debug_log "Stopping k3s processes..."
    pkill k3s || true

    sleep 2

    debug_log "k3s cluster stopped"
}

# Teardown k3s cluster completely
teardown_k3s_cluster() {
    debug_log "Tearing down k3s cluster..."

    if command -v k3s-uninstall.sh &> /dev/null; then
        debug_log "Running k3s uninstall script..."
        timeout 60 k3s-uninstall.sh || echo "k3s uninstall script timed out or failed"
    fi

    debug_log "Stopping k3s processes..."
    pkill k3s || true

    sleep 2

    debug_log "Cleaning up k3s data..."
    rm -rf /var/lib/rancher/k3s || true
    rm -rf /etc/rancher/k3s || true

    debug_log "k3s teardown complete"
}

restart_k3s_cluster() {
    debug_log "Restarting k3s cluster..."

    stop_k3s_cluster
    start_k3s_cluster
}

verify_k3s_cedana_runtime() {
    if cat /var/lib/rancher/k3s/agent/etc/containerd/config.toml.tmpl | grep -q 'cedana'; then
        return 0
    else
        return 1
    fi
}

wait_for_k3s_cedana_runtime() {
    debug_log "Waiting for cedana runtime to be configured in containerd..."
    local timeout=60
    local interval=1
    local elapsed=0

    until verify_k3s_cedana_runtime; do
        if (( elapsed >= timeout )); then
            debug_log "Timed out waiting for cedana runtime to be configured"
            return 1
        fi
        sleep "$interval"
        ((elapsed += interval))
    done

    debug_log "Cedana runtime is configured in containerd"
    return 0
}
