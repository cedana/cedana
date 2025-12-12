#!/usr/bin/env bash

##########################
### Kubernetes Helpers ###
##########################

install_kubectl() {
    if command -v kubectl &> /dev/null; then
        debug_log "kubectl is already installed"
        return 0
    fi
    debug_log "Installing kubectl..."
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)
            KC_ARCH="amd64"
            ;;
        aarch64|arm64)
            KC_ARCH="arm64"
            ;;
        *)
            error_log "Unsupported architecture for kubectl: $ARCH"
            return 1
            ;;
    esac
    curl -Lo /tmp/kubectl https://dl.k8s.io/release/v1.33.0/bin/linux/$KC_ARCH/kubectl
    install -m 0755 /tmp/kubectl /usr/local/bin/kubectl
    rm -f /tmp/kubectl
    mkdir -p ~/.kube
    debug_log "kubectl installed"
}

install_k9s () {
    if command -v k9s &> /dev/null; then
        debug_log "k9s is already installed"
        return 0
    fi
    debug_log "Installing k9s..."
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)
            K9S_ARCH="amd64"
            ;;
        aarch64|arm64)
            K9S_ARCH="arm64"
            ;;
        *)
            error_log "Unsupported architecture for k9s: $ARCH"
            return 1
            ;;
    esac
    wget https://github.com/derailed/k9s/releases/latest/download/k9s_linux_"$K9S_ARCH".deb -O /tmp/k9s.deb
    apt install -y /tmp/k9s.deb
    rm -f /tmp/k9s.deb
    debug_log "k9s installed"
}

# Generate a new spec from an existing one with a new name and namespace.
new_spec () {
    local spec="$1"
    local newname="$2"
    local newnamespace="${3:-default}"

    local newspec="/tmp/${newname}.yaml"

    # Get the oldname from the first "name:" line
    local oldname
    oldname=$(grep -m1 '^[[:space:]]*name:' "$spec" | sed -E 's/^[[:space:]]*name:[[:space:]]*"?([^"]+)"?/\1/')

    local oldnamespace
    oldnamespace=$(grep -m1 '^[[:space:]]*namespace:' "$spec" | sed -E 's/^[[:space:]]*namespace:[[:space:]]*"?([^"]+)"?/\1/')

    # Replace all 'name: <oldname>' patterns with the quoted newname
    sed -E "s/^([[:space:]\-]*name:[[:space:]]*)\"?$oldname\"?/\1\"$newname\"/g" "$spec" > "$newspec"

    # If oldnamespace is not empty, replace it; otherwise, add namespace under metadata
    if [[ -n "$oldnamespace" ]]; then
        sed -i -E "s/^([[:space:]]*namespace:[[:space:]]*)\"?$oldnamespace\"?/\1\"$newnamespace\"/g" "$newspec"
    else
        sed -i -E "/^metadata:/a\  namespace: \"$newnamespace\"" "$newspec"
    fi

    echo "$newspec"
}

cmd_pod_spec () {
    local namespace="${1:-default}"
    local name="$2"
    local image="$3"
    local args="$4"

    local spec=/tmp/pod-${name}.yaml
    cat > "$spec" << EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: "$namespace"
  labels:
    app: "$name"
spec:
  containers:
  - name: "$name"
    image: $image
    command: ["/bin/sh", "-c"]
EOF

    if [[ -n "$args" ]]; then
        # Print args as multi-line block, indent each line correctly
        printf "    args:\n" >> "$spec"
        printf "      - |\n" >> "$spec"
        while IFS= read -r line; do
            printf "        %s\n" "$line" >> "$spec"
        done <<< "$args"
    fi

    # debug cat "$spec"

    echo "$spec"
}

gpu_cmd_pod_spec () {
    local namespace="${1:-default}"
    local name="$2"
    local image="$3"
    local args="$4"

    local spec=/tmp/pod-${name}.yaml
    cat > "$spec" << EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: "$namespace"
  labels:
    app: "$name"
spec:
  runtimeClassName: cedana
  containers:
  - name: "$name"
    image: $image
    command: ["/bin/sh", "-c"]
    resources:
      limits:
        nvidia.com/gpu: 1
EOF

    if [[ -n "$args" ]]; then
        # Print args as multi-line block, indent each line correctly
        printf "    args:\n" >> "$spec"
        printf "      - |\n" >> "$spec"
        while IFS= read -r line; do
            printf "        %s\n" "$line" >> "$spec"
        done <<< "$args"
    fi

    debug cat "$spec"

    echo "$spec"
}

# List all restored pods in a given namespace.
# Does a simple filter on pod names containing "restored".
list_restored_pods() {
    local namespace="$1"
    kubectl get pods -n "$namespace" -o json | jq -r '.items[] | select(.metadata.name | contains("restored")) | .metadata.name'
}

# Gets a specific restored pod by its original pod name.
get_restored_pod() {
    local namespace="$1"
    local name="$2"

    local restored_pods
    restored_pods=$(list_restored_pods "$namespace")

    for restored_pod in $restored_pods; do
        # check if the pod contains a container with the same name
        if kubectl get pod "$restored_pod" -n "$namespace" -o jsonpath='{.spec.containers[*].name}' | grep -q "$name"; then
            echo "$restored_pod"
            return 0
        fi
    done

    error_log "No restored pods found for original pod $name"
    return 1
}

# Validates if a pod is ready with a timeout.
# Dumps the pod description if it fails to become ready.
validate_pod() {
    local namespace="$1"
    local name="$2"
    local timeout="$3"
    local stable_check_duration=5 # seconds to check if pod stays Ready
    local stable_check_interval=1 # polling interval

    if ! kubectl get pod "$name" -n "$namespace" &>/dev/null; then
        debug_log "Pod $name does not exist in namespace $namespace"
        return 1
    fi

    debug_log "Waiting for pod $name in namespace $namespace to become Ready"

    if kubectl wait --for=condition=Ready pod/"$name" -n "$namespace" --timeout="$timeout" 2>/dev/null; then
        debug_log "Pod $name is Ready"

        # Additional check: Verify pod stays Ready for a while
        local ready_consistently=true
        local elapsed=0
        while [ $elapsed -lt $stable_check_duration ]; do
            status=$(kubectl get pod "$name" -n "$namespace" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')
            if [ "$status" != "True" ]; then
                ready_consistently=false
                break
            fi
            sleep $stable_check_interval
            ((elapsed+=stable_check_interval))
        done

        if $ready_consistently; then
            debug_log "Pod $name stayed Ready for $stable_check_duration seconds"
            return 0
        else
            error_log "Pod $name did not stay Ready for the required duration"
            # fall through to error handling below
        fi
    fi

    error_log "Timed out waiting for pod $name to become Ready"
    error_log "Dumping pod description for $name in namespace $namespace:"
    error kubectl describe pod "$name" -n "$namespace" || error_lod "No description available"
    # error_log "Events from pod $name in namespace $namespace:"
    # error echo -e "$(kubectl get events --field-selector involvedObject.kind=Pod,involvedObject.name="$name" -n "$namespace" -o json | jq '.items[].message')"
    error_log "Logs from pod $name in namespace $namespace:"
    error kubectl logs "$name" -n "$namespace" --tail=1000 --prefix=true 2>/dev/null || echo "No logs available"
    return 1
}

# Tails logs of all pods in a given namespace, waiting for them to be Running first.
tail_all_logs() {
    local namespace="$1"
    local timeout="${2:-120}"
    local tail=${3:--1}

    wait_for_cmd "$timeout" "kubectl get pods -n $namespace | grep -q ."

    debug_log "Waiting for all pods in namespace $namespace to be Running (timeout: $timeout seconds)"

    kubectl get pods -n "$namespace" -o name | xargs -P0 -I{} kubectl wait --for=jsonpath='{.status.phase}=Running' -n "$namespace" --timeout="$timeout"s {} || {
        error_log "Failed to wait for all pods in namespace $namespace to be Running"
        for pod in $(kubectl get pods -n "$namespace" -o name); do
            error_log "Pod $pod status: $(kubectl get "$pod" -n "$namespace" -o jsonpath='{.status.phase}')"
            kubectl describe "$pod" -n "$namespace" | awk '/^Events:/,0' | while read -r line; do
                error_log "$line"
            done
        done
        return 1
    }

    debug_log "Tailing all logs in namespace $namespace"

    debug "kubectl get pods -n $namespace -o name | xargs -P0 -I{} kubectl logs -n $namespace -f --tail $tail {}"
}

# Logs of all pods in a given namespace, waiting for them to be Running first.
all_logs() {
    local namespace="$1"
    local timeout="${2:-120}"
    local tail=${3:--1}

    wait_for_cmd "$timeout" "kubectl get pods -n $namespace | grep -q ."

    debug_log "Waiting for all pods in namespace $namespace to be Running (timeout: $timeout seconds)"

    kubectl get pods -n "$namespace" -o name | xargs -P0 -I{} kubectl wait --for=jsonpath='{.status.phase}=Running' -n "$namespace" --timeout="$timeout"s {} || {
        error_log "Failed to wait for all pods in namespace $namespace to be Running"
        for pod in $(kubectl get pods -n "$namespace" -o name); do
            error_log "Pod $pod status: $(kubectl get "$pod" -n "$namespace" -o jsonpath='{.status.phase}')"
            kubectl describe "$pod" -n "$namespace" | awk '/^Events:/,0' | while read -r line; do
                error_log "$line"
            done
        done
        return 1
    }

    debug_log "Tailing all logs in namespace $namespace"

    debug "kubectl get pods -n $namespace -o name | xargs -P0 -I{} kubectl logs -n $namespace --tail $tail {}"
}

# Waits for all pods in a given namespace to be Ready.
wait_for_ready() {
    local namespace="$1"
    local timeout="${2:-120}"

    wait_for_cmd "$timeout" "kubectl get pods -n $namespace | grep -q ."

    debug_log "Waiting for all pods in namespace $namespace to be Ready (timeout: $timeout seconds)"

    kubectl wait --for=condition=Ready --for=jsonpath='{.status.phase}'=Succeeded pod --all -n "$namespace" --timeout="$timeout"s || {
        error_log "Failed to wait for all pods in namespace $namespace to be Ready"
        for pod in $(kubectl get pods -n "$namespace" -o name); do
            error_log "Pod $pod status: $(kubectl get "$pod" -n "$namespace" -o jsonpath='{.status.phase}')"
            kubectl describe "$pod" -n "$namespace" | awk '/^Events:/,0' | while read -r line; do
                error_log "$line"
            done
            error_log "Logs from pod $pod in namespace $namespace:"
            error kubectl logs "$pod" -n "$namespace" --tail=1000 --
        done
        return 1
    }

    debug_log "All pods in namespace $namespace are Ready"
}

create_namespace() {
    local namespace="$1"

    if kubectl get namespace "$namespace" &>/dev/null; then
        debug_log "Namespace $namespace already exists"
        return 0
    fi

    debug_log "Creating namespace $namespace..."
    kubectl create namespace "$namespace" || {
        error_log "Failed to create namespace $namespace"
        return 1
    }
    debug_log "Namespace $namespace created"
}

delete_namespace() {
    local namespace="$1"

    if ! kubectl get namespace "$namespace" &>/dev/null; then
        debug_log "Namespace $namespace does not exist"
        return 0
    fi

    debug_log "Deleting namespace $namespace"
    kubectl delete namespace "$namespace" --wait=true "${@:2}" || {
        error_log "Failed to delete namespace $namespace"
        return 1
    }
    debug_log "Namespace $namespace deleted"
}

# Get pod UID (unique identifier) from pod name and namespace
# @param $1: Pod name
# @param $2: Namespace
# Returns: Pod UID
get_pod_id() {
    local pod_name="$1"
    local namespace="$2"

    if [ -z "$pod_name" ] || [ -z "$namespace" ]; then
        error_log "get_pod_id requires pod_name and namespace"
        return 1
    fi

    local pod_uid
    pod_uid=$(kubectl get pod "$pod_name" -n "$namespace" -o jsonpath='{.metadata.uid}' 2>/dev/null)

    if [ -z "$pod_uid" ]; then
        error_log "Failed to get pod UID for pod '$pod_name' in namespace '$namespace'"
        return 1
    fi

    echo "$pod_uid"
    return 0
}
