#!/usr/bin/env bash

##########################
### Kubernetes Helpers ###
##########################

# Install kubectl if not available
install_kubectl() {
    debug_log "Installing kubectl..."

    if command -v kubectl &>/dev/null; then
        debug_log "kubectl already installed"
        return 0
    fi

    # Try to install kubectl
    local kubectl_version=$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)
    curl -LO "https://storage.googleapis.com/kubernetes-release/release/${kubectl_version}/bin/linux/amd64/kubectl"
    if [ $? -eq 0 ]; then
        chmod +x kubectl
        sudo mv kubectl /usr/local/bin/
        # Refresh PATH to ensure kubectl is found
        export PATH="$PATH:/usr/local/bin"
        if command -v kubectl &>/dev/null; then
            debug_log "kubectl installed successfully"
            return 0
        else
            debug_log "Error: kubectl installation completed but kubectl command not found in PATH"
            debug_log "Current PATH: $PATH"
            return 1
        fi
    fi

    debug_log "Warning: Failed to install kubectl, will continue without it"
    return 1
}

# Generate a new spec from an existing one with a new name.
new_spec() {
    local spec="$1"
    local newname="$2"

    local newspec="/tmp/${newname}.yaml"

    # Get the oldname from the first "name:" line
    local oldname
    oldname=$(grep -m1 '^[[:space:]]*name:' "$spec" | sed -E 's/^[[:space:]]*name:[[:space:]]*"?([^"]+)"?/\1/')

    # Replace all 'name: <oldname>' patterns with the quoted newname
    sed -E "s/^([[:space:]\-]*name:[[:space:]]*)\"?$oldname\"?/\1\"$newname\"/g" "$spec" >"$newspec"
    debug cat "$newspec"

    echo "$newspec"
}

simple_pod_spec() {
    local name="$1"
    local image="$2"
    local command="$3"
    local args="$4"
    local namespace="${5:-default}"

    local spec=/tmp/pod-${name}.yaml
    cat >"$spec" <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: $namespace
  labels:
    app: "$name"
spec:
  restartPolicy: Never
  containers:
  - name: $name
    image: $image
EOF
    if [[ -n "$command" ]]; then
        cat >>"$spec" <<EOF
        command: ${command}
EOF
    fi
    if [[ -n "$args" ]]; then
        cat >>"$spec" <<EOF
        args: ${args}
EOF
    fi

    echo "$spec"
}

gpu_pod_spec() {
    local name="$1"
    local image="$2"
    local command="$3"
    local args="$4"
    local namespace="${5:-default}"

    local spec=/tmp/pod-${name}.yaml
    cat >"$spec" <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: $namespace
  labels:
    app: "$name"
spec:
  restartPolicy: Never
  runtimeClassName: cedana # for Cedana GPU C/R support
  containers:
  - name: $name
    image: $image
EOF
    if [[ -n "$command" ]]; then
        cat >>"$spec" <<EOF
        command: ${command}
EOF
    fi
    if [[ -n "$args" ]]; then
        cat >>"$spec" <<EOF
        args: ${args}
EOF
    fi

    echo "$spec"
}

# List all restored pods in a given namespace.
# Does a simple filter on pod names containing "restored".
list_restored_pods() {
    local namespace="$1"

    # Ensure kubectl is available
    if ! command -v kubectl &>/dev/null; then
        install_kubectl
        if [ $? -ne 0 ]; then
            debug_log "Error: kubectl is required for listing restored pods"
            return 1
        fi
    fi

    kubectl get pods -n "$namespace" -o json | jq -r '.items[] | select(.metadata.name | contains("restored")) | .metadata.name'
}

# Gets a specific restored pod by its original pod name.
get_restored_pod() {
    local namespace="$1"
    local name="$2"

    # Ensure kubectl is available
    if ! command -v kubectl &>/dev/null; then
        install_kubectl
        if [ $? -ne 0 ]; then
            debug_log "Error: kubectl is required for getting restored pod"
            return 1
        fi
    fi

    local restored_pods
    restored_pods=$(list_restored_pods "$namespace")

    for restored_pod in $restored_pods; do
        # check if the pod contains a container with the same name
        if kubectl get pod "$restored_pod" -n "$namespace" -o jsonpath='{.spec.containers[*].name}' | grep -q "$name"; then
            echo "$restored_pod"
            return 0
        fi
    done

    debug_log "No restored pods found for original pod $name"
    return 1
}

# Validates if a pod is ready with a timeout.
# Dumps the pod description if it fails to become ready.
validate_pod() {
    local namespace="$1"
    local name="$2"
    local timeout="$3"

    # Ensure kubectl is available
    if ! command -v kubectl &>/dev/null; then
        install_kubectl
        if [ $? -ne 0 ]; then
            debug_log "Error: kubectl is required for pod validation"
            return 1
        fi
    fi

    if ! kubectl get pod "$name" -n "$namespace" &>/dev/null; then
        debug_log "Pod $name does not exist in namespace $namespace"
        return 1
    fi

    debug_log "Waiting for pod $name in namespace $namespace to become Ready"

    if kubectl wait --for=condition=Ready pod/"$name" -n "$namespace" --timeout="$timeout" 2>/dev/null; then
        debug_log "Pod $name is Ready"
        return 0
    fi

    debug_log "Timed out waiting for pod $name to become Ready"
    debug echo -e "$(kubectl get events --field-selector involvedObject.kind=Pod,involvedObject.name="$name" -n "$namespace" -o json | jq '.items[].message')"
    return 1
}
