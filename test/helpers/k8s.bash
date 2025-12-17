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

setup_samples() {
    debug_log "Setting up SAMPLES_DIR..."
    if [ -z "$SAMPLES_DIR" ]; then
        if [ -d "../cedana-samples/kubernetes" ]; then
            SAMPLES_DIR="../cedana-samples/kubernetes"
        elif [ -d "/cedana-samples/kubernetes" ]; then
            SAMPLES_DIR="/cedana-samples/kubernetes"
        elif [ -d "/tmp/cedana-samples/kubernetes" ]; then
            SAMPLES_DIR="/tmp/cedana-samples/kubernetes"
        else
            # Clone cedana-samples repo to /tmp
            if git clone --depth 1 https://github.com/cedana/cedana-samples.git /tmp/cedana-samples 2>/dev/null; then
                SAMPLES_DIR="/tmp/cedana-samples/kubernetes"
            else
                SAMPLES_DIR=""
            fi
        fi
    fi
    export SAMPLES_DIR
    debug_log "SAMPLES_DIR is set to $SAMPLES_DIR"
}

# Generate a new spec from an existing one with a new name and namespace.
new_spec () {
    local spec="$1"
    local newname="$2"
    local newnamespace="${3:-$NAMESPACE}"

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
    local image="$1"
    local args="$2"
    local namespace="${3:-$NAMESPACE}"

    local name
    name=$(unix_nano)

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

    echo "$spec"
}

cmd_pod_spec_gpu () {
    local image="$1"
    local args="$2"
    local gpus="${3:-1}"
    local namespace="${4:-$NAMESPACE}"

    local name
    name=$(unix_nano)

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
        nvidia.com/gpu: "$gpus"
EOF

    if [[ -n "$args" ]]; then
        # Print args as multi-line block, indent each line correctly
        printf "    args:\n" >> "$spec"
        printf "      - |\n" >> "$spec"
        while IFS= read -r line; do
            printf "        %s\n" "$line" >> "$spec"
        done <<< "$args"
    fi

    echo "$spec"
}

# Use an existing pod spec file, modify its namespace to the
# test namespace, and return the new spec path.
pod_spec() {
    local source_file="$1"
    local namespace="${2:-$NAMESPACE}"
    local unique_id
    unique_id="$(unix_nano)"

    # Check if source file exists
    if [ ! -f "$source_file" ]; then
        error_log "Error: spec file $source_file does not exist"
        return 1
    fi

    # Check if the spec is a Pod
    local kind
    kind=$(grep -E "^kind:" "$source_file" | head -1 | awk '{print $2}' | tr -d '"' | tr -d "'")
    if [ "$kind" != "Pod" ]; then
        error_log "Error: spec file $source_file is not a Pod (kind: $kind)"
        return 1
    fi

    local temp_spec="/tmp/test-spec-${unique_id}.yaml"

    sed -e "s/namespace: default/namespace: $namespace/g" \
        -e "s/namespace: .*/namespace: $namespace/g" \
        "$source_file" > "$temp_spec"

    if ! grep -q "namespace:" "$temp_spec"; then
        sed -i '/^metadata:/a\  namespace: '"$namespace"'' "$temp_spec"
    fi

    echo "$temp_spec"
}


# List all restored pods in a given namespace.
# Does a simple filter on pod names containing "restored".
list_restored_pods() {
    local namespace="${1:-$NAMESPACE}"
    kubectl get pods -n "$namespace" -o json | jq -r '.items[] | select(.metadata.name | contains("restored")) | .metadata.name'
}

# Gets a specific restored pod by its original pod name.
get_restored_pod() {
    local name="$1"
    local namespace="${2:-$NAMESPACE}"

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
    local name="$1"
    local timeout="${2:-120}"s
    local namespace="${3:-$NAMESPACE}"
    local stable_check_duration=5 # seconds to check if pod stays Ready
    local stable_check_interval=1 # polling interval

    if ! kubectl get pod "$name" -n "$namespace" &>/dev/null; then
        error_log "Pod $name does not exist in namespace $namespace"
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

    error_log "Timed out waiting for pod $name to become Ready after $timeout seconds"
    error_log "Dumping pod description for $name in namespace $namespace:"
    error kubectl describe pod "$name" -n "$namespace" || error_log "No description available"
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

    # Get only Running pods
    local running_pods
    running_pods=$(kubectl get pods -n "$namespace" --field-selector=status.phase=Running -o name 2>/dev/null)

    if [ -z "$running_pods" ]; then
        debug_log "No Running pods found in namespace $namespace"
        return 0
    fi

    echo "$running_pods" | xargs -r kubectl wait --for=condition=Ready -n "$namespace" --timeout="$timeout"s || {
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
    local name="$1"
    local namespace="$2"

    if [ -z "$name" ] || [ -z "$namespace" ]; then
        error_log "get_pod_id requires pod_name and namespace"
        return 1
    fi

    local pod_uid
    pod_uid=$(kubectl get pod "$name" -n "$namespace" -o jsonpath='{.metadata.uid}' 2>/dev/null)

    if [ -z "$pod_uid" ]; then
        error_log "Failed to get pod UID for pod '$name' in namespace '$namespace'"
        return 1
    fi

    echo "$pod_uid"
    return 0
}

# Get available GPU count from any schedulable node
get_available_gpus() {
    local gpu_count
    gpu_count=$(kubectl get nodes -o json | jq '[.items[].status.allocatable["nvidia.com/gpu"] // "0" | tonumber] | add' 2>/dev/null)
    echo "${gpu_count:-0}"
}

# Check if required GPUs are available
is_gpu_available() {
    local required_gpus="$1"
    local available_gpus
    available_gpus=$(get_available_gpus)

    if [ "$available_gpus" -ge "$required_gpus" ]; then
        return 0
    else
        return 1
    fi
}

# Count total GPUs required by a spec file
get_required_gpus() {
    local spec="$1"
    local gpu_count
    gpu_count=$(grep -o "nvidia.com/gpu.*[0-9]" "$spec" 2>/dev/null | grep -o "[0-9]*" | awk '{sum+=$1} END {print sum}')
    echo "${gpu_count:-0}"
}

# Get pod name from a created resource
get_created_pod_name() {
    local spec="$1"
    local namespace="$2"
    local timeout="${3:-60}"

    local name
    name=$(grep -E "^\s*name:" "$spec" | head -1 | awk '{print $2}' | tr -d '"')
    local generate_name
    generate_name=$(grep -E "^\s*generateName:" "$spec" | head -1 | awk '{print $2}' | tr -d '"')

    if [ -n "$name" ]; then
        echo "$name"
        return 0
    elif [ -n "$generate_name" ]; then
        for _ in $(seq 1 "$timeout"); do
            local pod
            pod=$(kubectl get pods -n "$namespace" --no-headers 2>/dev/null | grep "^${generate_name}" | head -1 | awk '{print $1}')
            if [ -n "$pod" ]; then
                echo "$pod"
                return 0
            fi
            sleep 1
        done
        error_log "Timeout waiting for pod with prefix $generate_name"
        return 1
    else
        error_log "Could not determine pod name from spec"
        return 1
    fi
}

# Run a complete test cycle based on action and pod spec.
# @param $1: Action (DEPLOY, DUMP, RESTORE or DUMP_RESTORE)
# @param $2: Pod spec file path
# @param $3: Namespace (optional, defaults to $NAMESPACE)
test_pod_spec() {
    local action="$1"
    local spec="$2"
    local namespace="${3:-$NAMESPACE}"

    local required_gpus
    required_gpus=$(get_required_gpus "$spec")

    local available_gpus
    available_gpus=$(get_available_gpus)

    if [ "$available_gpus" -lt "$required_gpus" ]; then
        skip "Insufficient GPUs: need $required_gpus, have $available_gpus"
    fi

    local dump_wait_time=5
    local pod_timeout=300
    if [ "$required_gpus" -gt 0 ]; then
        dump_wait_time=60
        pod_timeout=1300
    fi

    local container_name
    container_name=$(grep -A1 "containers:" "$spec" | grep "name:" | head -1 | awk '{print $3}' | tr -d '"' | tr -d "'")
    debug_log "Container name from spec: $container_name"

    # Deploy
    debug_log "Deploying from $spec..."
    if grep -q "generateName:" "$spec"; then
        kubectl create -f "$spec"
    else
        kubectl apply -f "$spec"
    fi

    # Get pod name
    local name
    name=$(get_created_pod_name "$spec" "$namespace" 60)
    if [ -z "$name" ]; then
        error_log "Failed to get pod name"
        return 1
    fi

    # Wait for pod to become ready
    validate_pod "$name" "$pod_timeout"

    debug_log "Deployed pod $name deployed successfully"

    if [ "$action" = "DEPLOY" ]; then
        kubectl delete pod "$name" -n "$namespace" --wait=true || true
        return 0
    fi

    sleep "$dump_wait_time"

    # Checkpoint
    debug_log "Dumping pod $name..."
    local pod_id
    pod_id=$(get_pod_id "$name" "$namespace")

    local checkpoint_output
    checkpoint_output=$(checkpoint_pod "$pod_id")
    local checkpoint_status=$?

    if [ $checkpoint_status -ne 0 ]; then
        error_log "Checkpoint failed: $checkpoint_output"
        kubectl delete pod "$name" -n "$namespace" --wait=true || true
        return 1
    fi

    local action_id="$checkpoint_output"
    validate_action_id "$action_id" || {
        error_log "Invalid action ID: $action_id"
        kubectl delete pod "$name" -n "$namespace" --wait=true || true
        return 1
    }

    poll_action_status "$action_id" "checkpoint" || {
        kubectl delete pod "$name" -n "$namespace" --wait=true || true
        return 1
    }

    debug_log "Dumped pod $name successfully"

    if [ "$action" = "DUMP" ]; then
        kubectl delete pod "$name" -n "$namespace" --wait=true || true
        return 0
    fi

    debug_log "Deleting original pod before restore..."
    kubectl delete pod "$name" -n "$namespace" --wait=true || true
    name=""

    debug_log "Restoring pod..."

    local restore_output
    restore_output=$(restore_pod "$action_id" "$CLUSTER_ID")
    local restore_status=$?

    if [ $restore_status -ne 0 ]; then
        error_log "Restore failed: $restore_output"
        return 1
    fi

    local restore_action_id="$restore_output"
    validate_action_id "$restore_action_id" || {
        error_log "Invalid restore action ID: $restore_action_id"
        return 1
    }

    local name_restored
    name_restored=$(wait_for_cmd 30 get_restored_pod "$name" "$namespace")

    debug_log "Restore starting for $name_restored..."

    # Wait for pod to become ready
    validate_pod "$name_restored" "$pod_timeout"

    debug_log "Restored pod $name_restored successfully"

    kubectl delete pod "$name_restored" -n "$namespace" --wait=true || true
}
