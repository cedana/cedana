#!/usr/bin/env bash

##########################
### Kubernetes Helpers ###
##########################

install_kubectl() {
    if command -v kubectl &>/dev/null; then
        debug_log "kubectl is already installed"
        return 0
    fi
    debug_log "Installing kubectl..."
    ARCH=$(uname -m)
    case "$ARCH" in
    x86_64)
        KC_ARCH="amd64"
        ;;
    aarch64 | arm64)
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

install_k9s() {
    if command -v k9s &>/dev/null; then
        debug_log "k9s is already installed"
        return 0
    fi
    debug_log "Installing k9s..."
    ARCH=$(uname -m)
    case "$ARCH" in
    x86_64)
        K9S_ARCH="amd64"
        ;;
    aarch64 | arm64)
        K9S_ARCH="arm64"
        ;;
    *)
        error_log "Unsupported architecture for k9s: $ARCH"
        return 1
        ;;
    esac
    wget -q https://github.com/derailed/k9s/releases/latest/download/k9s_linux_"$K9S_ARCH".deb -O /tmp/k9s.deb
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

# Use an existing pod spec file, modify its namespace to the
# test namespace, and return the new spec path.
# Optional third parameter: workload name to include in the pod name
# e.g., pod_spec "/path/to/spec.yaml" "test" "gromacs" -> test-cuda-gromacs-1737394521234567890
pod_spec() {
    local source_file="$1"
    local namespace="${2:-$NAMESPACE}"
    local workload_name="${3:-}"

    # Check if source file exists
    if [ ! -f "$source_file" ]; then
        error_log "Error: spec file $source_file does not exist"
        return 1
    fi

    local unique_id
    local prefix="test"
    if grep -q "nvidia.com/gpu" "$source_file"; then
        prefix="test-cuda"
    fi

    if [ -n "$workload_name" ]; then
        unique_id="${prefix}-${workload_name}-$(unix_nano)"
    else
        unique_id="${prefix}-$(unix_nano)"
    fi

    # Check if the spec is a Pod
    local kind
    kind=$(grep -E "^kind:" "$source_file" | head -1 | awk '{print $2}' | tr -d '"' | tr -d "'")
    if [ "$kind" != "Pod" ]; then
        error_log "Error: spec file $source_file is not a Pod (kind: $kind)"
        return 1
    fi

    local temp_spec="/tmp/test-spec-${unique_id}.yaml"
    cp "$source_file" "$temp_spec"

    sed -i \
        -e "/^metadata:/,/^[^ ]/ s/^  name:[[:space:]]*.*/  name: $unique_id/" \
        -e "/^metadata:/,/^[^ ]/ s/^  generateName:[[:space:]]*.*/  name: $unique_id/" \
        -e "s/namespace: default/namespace: $namespace/g" \
        -e "s/namespace: .*/namespace: $namespace/g" \
        "$temp_spec"

    if ! grep -q "namespace:" "$temp_spec"; then
        sed -i '/^metadata:/a\  namespace: '"$namespace"'' "$temp_spec"
    fi

    debug cat "$temp_spec"

    echo "$temp_spec"
}

cmd_pod_spec() {
    local image="$1"
    local args="$2"
    local namespace="${3:-$NAMESPACE}"
    local workload_name="${4:-}"

    local name
    if [ -n "$workload_name" ]; then
        name="test-${workload_name}-$(unix_nano)"
    else
        name="test-$(unix_nano)"
    fi

    local spec=/tmp/pod-${name}.yaml
    cat >"$spec" <<EOF
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
        printf "    args:\n" >>"$spec"
        printf "      - |\n" >>"$spec"
        while IFS= read -r line; do
            printf "        %s\n" "$line" >>"$spec"
        done <<<"$args"
    fi

    echo "$spec"
}

cmd_pod_spec_gpu() {
    local image="$1"
    local args="$2"
    local gpus="${3:-1}"
    local namespace="${4:-$NAMESPACE}"
    local workload_name="${5:-}"

    local name
    if [ -n "$workload_name" ]; then
        name="test-cuda-${workload_name}-$(unix_nano)"
    else
        name="test-cuda-$(unix_nano)"
    fi

    local spec=/tmp/pod-${name}.yaml
    cat >"$spec" <<EOF
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
        printf "    args:\n" >>"$spec"
        printf "      - |\n" >>"$spec"
        while IFS= read -r line; do
            printf "        %s\n" "$line" >>"$spec"
        done <<<"$args"
    fi

    echo "$spec"
}

cmd_pvc_spec() {
    local size="$1"
    local name="$2"
    local storage_class="$3"
    local namespace="${4:-$NAMESPACE}"

    if [[ -z "$name" ]]; then
        name="pvc-$(unix_nano)"
    fi

    local spec=/tmp/pvc-${name}.yaml
    cat >"$spec" <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: "$name"
  namespace: "$namespace"
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: $size
EOF

    # Use default StorageClass when omitted
    if [[ -n "$storage_class" ]]; then
        printf "  storageClassName: %s\n" "$storage_class" >>"$spec"
    fi

    echo "$spec"
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

    name=${name%%-restored}

    local restored_pods
    restored_pods=$(list_restored_pods "$namespace")

    for restored_pod in $restored_pods; do
        # check if the pod contains a container with the same name
        if kubectl get pod "$restored_pod" -n "$namespace" -o name | grep -q "$name"; then
            echo "$restored_pod"
            return 0
        fi
    done

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
            ((elapsed += stable_check_interval))
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

    kubectl get pods -n "$namespace" -o name | while IFS= read -r pod; do
        debug kubectl logs -n "$namespace" -f --tail "$tail" "$pod" &
    done
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

    # Get all pods
    local pods
    pods=$(kubectl get pods -n "$namespace" -o name 2>/dev/null)

    if [ -z "$pods" ]; then
        debug_log "No Running pods found in namespace $namespace"
        return 0
    fi

    echo "$pods" | xargs -r kubectl wait --for=condition=Ready -n "$namespace" --timeout="$timeout"s || {
        error_log "Failed to wait for all pods in namespace $namespace to be Ready"
        for pod in $(kubectl get pods -n "$namespace" -o name); do
            error_log "Pod $pod status: $(kubectl get "$pod" -n "$namespace" -o jsonpath='{.status.phase}')"
            kubectl describe "$pod" -n "$namespace" | awk '/^Events:/,0' | while read -r line; do
                error_log "$line"
            done
            error_log "Logs from pod $pod in namespace $namespace:"
            error_log kubectl logs "$pod" -n "$namespace" --tail=1000 || true
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
    # Only count GPU limits (not requests) to avoid double-counting
    # Look for "nvidia.com/gpu: N" under a "limits:" section
    gpu_count=$(awk '/limits:/,/requests:|env:|^[^ ]/ {if (/nvidia\.com\/gpu:/) print $NF}' "$spec" 2>/dev/null | awk '{sum+=$1} END {print sum}')
    echo "${gpu_count:-0}"
}

# Get pod name from a created resource
get_created_pod() {
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

# Run a complete test cycle based on action sequence and pod spec.
# @param $1: Action sequence (e.g., DEPLOY, DUMP_RESTORE, DUMP_RESTORE_DUMP_RESTORE)
#            Actions are separated by underscores and executed in order.
#            Valid actions: DEPLOY, DUMP, RESTORE
# @param $2: Pod spec file path
# @param $3: Pod timeout (optional, defaults to 60)
# @param $4: Dump wait time in seconds (optional, defaults to 5) - ignored if dump_trigger is set
# @param $5: Dump timeout (optional, defaults to 30)
# @param $6: Namespace (optional, defaults to $NAMESPACE)
# @param $7: Dump trigger string (optional) - if set, waits for this string in logs instead of using dump_wait_time
# @param $8: Trigger timeout in seconds (optional, defaults to 300) - only used if dump_trigger is set
# @param $9: Post-trigger wait time in seconds (optional, defaults to 0) - additional wait after trigger is found
test_pod_spec() {
    local action_sequence="$1"
    local spec="$2"
    local pod_timeout="${3:-60}"
    local dump_wait_time="${4:-5}"
    local dump_timeout="${5:-30}"
    local namespace="${6:-$NAMESPACE}"
    local dump_trigger="${7:-}"
    local trigger_timeout="${8:-300}"
    local post_trigger_wait="${9:-0}"

    local required_gpus
    required_gpus=$(get_required_gpus "$spec")

    local available_gpus
    available_gpus=$(get_available_gpus)

    if [ "$available_gpus" -lt "$required_gpus" ]; then
        skip "Insufficient GPUs: need $required_gpus, have $available_gpus"
    fi

    local container_name
    container_name=$(grep -A1 "containers:" "$spec" | grep "name:" | head -1 | awk '{print $3}' | tr -d '"' | tr -d "'")
    debug_log "Container name from spec: $container_name"

    # Parse actions from the sequence (split by underscore)
    IFS='_' read -ra actions <<<"$action_sequence"

    local name=""
    local original_name=""
    local action_id=""
    local deployed=false
    local error=""

    for action in "${actions[@]}"; do
        case "$action" in
        DEPLOY)
            if [ "$deployed" = true ]; then
                error="Cannot DEPLOY twice - pod already deployed"
                break
            fi

            debug_log "Deploying from $spec..."
            if grep -q "generateName:" "$spec"; then
                kubectl create -f "$spec"
            else
                kubectl apply -f "$spec"
            fi

            name=$(get_created_pod "$spec" "$namespace" 30)
            if [ -z "$name" ]; then
                error="Failed to get pod name"
                break
            fi

            validate_pod "$name" "$pod_timeout" || {
                error="Pod $name failed to become Ready"
                break
            }

            debug_log "Deployed pod $name successfully"
            original_name="$name"
            deployed=true
            ;;

        DUMP)
            if [ "$deployed" = false ]; then
                error="Cannot DUMP - no pod deployed yet"
                break
            fi
            if [ -z "$name" ]; then
                error="Cannot DUMP - no active pod"
                break
            fi

            # Wait for log trigger if provided, otherwise use fixed sleep
            if [ -n "$dump_trigger" ]; then
                wait_for_log_trigger "$name" "$dump_trigger" "$trigger_timeout" "$namespace" || {
                    error="Timeout waiting for dump trigger '$dump_trigger' in pod $name"
                    break
                }
                if [ "$post_trigger_wait" -gt 0 ]; then
                    debug_log "Waiting ${post_trigger_wait}s after trigger before dump..."
                    sleep "$post_trigger_wait"
                fi
            else
                sleep "$dump_wait_time"
            fi

            debug_log "Dumping pod $name..."
            local pod_id
            pod_id=$(get_pod_id "$name" "$namespace")

            local checkpoint_output
            checkpoint_output=$(checkpoint_pod "$pod_id")
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

            poll_action_status "$action_id" "checkpoint" "$dump_timeout" || {
                error="Checkpoint action $action_id did not complete successfully"
                break
            }

            debug_log "Dumped pod $name successfully (action_id: $action_id)"

            if [ -n "$dump_trigger" ]; then
                debug_log "Verifying pod $name resumes after checkpoint..."
                wait_for_new_log_trigger "$name" "$dump_trigger" 300 "$namespace" || {
                    error="Pod $name did not resume training after checkpoint (no new '$dump_trigger' in logs)"
                    break
                }
                debug_log "Pod $name successfully resumed after checkpoint"
            fi
            ;;

        RESTORE)
            if [ -z "$action_id" ]; then
                error="Cannot RESTORE - no checkpoint action ID available"
                break
            fi

            debug_log "Deleting pod $name before restore..."
            kubectl delete pod "$name" -n "$namespace" --wait=true || {
                error="Failed to delete pod $name before restore"
                break
            }
            deployed=false

            debug_log "Restoring pod from action_id $action_id..."

            local restore_output
            restore_output=$(restore_pod "$action_id" "$CLUSTER_ID")
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

            name=$(wait_for_cmd 30 get_restored_pod "$original_name" "$namespace")

            debug_log "Restore starting for $name..."

            validate_pod "$name" "$pod_timeout"
            debug_log "Restored pod $name successfully"
            original_name="$name"
            deployed=true
            ;;

        *)
            error="Unknown action: $action"
            break
            ;;
        esac
    done

    if [ -n "$error" ]; then
        error_log "$error"
    fi

    # Fetch Pod logs before Deleting
    if [ -n "$name" ]; then
        debug_log "Fetching logs from pod $name..."
        error_log kubectl logs "$name" -n "$namespace" --tail=500 || true
    fi

    # Clean up the final pod
    if [ -n "$name" ]; then
        debug_log "Cleaning up pod $name..."
        kubectl delete pod "$name" -n "$namespace"
    fi

    if [ -n "$error" ]; then
        return 1
    else
        return 0
    fi
}

# Wait for a specific string to appear in pod logs.
# @param $1: Pod name
# @param $2: Trigger string to grep for
# @param $3: Timeout in seconds (optional, defaults to 300)
# @param $4: Namespace (optional, defaults to $NAMESPACE)
# Returns: 0 if trigger found, 1 if timeout
wait_for_log_trigger() {
    local name="$1"
    local trigger="$2"
    local timeout="${3:-300}"
    local namespace="${4:-$NAMESPACE}"
    local poll_interval=2

    debug_log "Waiting for trigger '$trigger' in pod $name logs (timeout: ${timeout}s)"

    local elapsed=0
    while [ $elapsed -lt "$timeout" ]; do
        if kubectl logs "$name" -n "$namespace" 2>/dev/null | grep -qi "$trigger"; then
            debug_log "Found trigger '$trigger' in pod $name after ${elapsed}s"
            return 0
        fi
        sleep $poll_interval
        ((elapsed += poll_interval))
    done

    error_log "Timeout waiting for trigger '$trigger' in pod $name logs after ${timeout}s"
    return 1
}

# Wait for a new occurrence of a trigger string in pod logs (after checkpoint).
# Tests for successful unfreeze
# @param $1: Pod name
# @param $2: Trigger string to grep for
# @param $3: Timeout in seconds (optional, defaults to 300)
# @param $4: Namespace (optional, defaults to $NAMESPACE)
# Returns: 0 if new trigger found, 1 if timeout
wait_for_new_log_trigger() {
    local name="$1"
    local trigger="$2"
    local timeout="${3:-300}"
    local namespace="${4:-$NAMESPACE}"
    local poll_interval=2

    debug_log "Waiting for NEW trigger '$trigger' in pod $name logs (timeout: ${timeout}s)"

    local initial_count
    initial_count=$(kubectl logs "$name" -n "$namespace" --all-containers=true 2>/dev/null | grep -ci "$trigger" || echo "0")
    initial_count=${initial_count##*$'\n'} # Take last line only (handles multi-container output)
    debug_log "Initial count of '$trigger' in logs: $initial_count"

    local elapsed=0
    while [ $elapsed -lt "$timeout" ]; do
        local current_count
        current_count=$(kubectl logs "$name" -n "$namespace" --all-containers=true 2>/dev/null | grep -ci "$trigger" || echo "0")
        current_count=${current_count##*$'\n'} # Take last line only (handles multi-container output)

        if [ "$current_count" -gt "$initial_count" ]; then
            debug_log "Found NEW trigger '$trigger' in pod $name after ${elapsed}s (count: $initial_count -> $current_count)"
            return 0
        fi
        sleep $poll_interval
        ((elapsed += poll_interval))
    done

    error_log "Timeout waiting for NEW trigger '$trigger' in pod $name logs after ${timeout}s"
    return 1
}

# Simulate spot interruption by terminating the EC2 instance
simulate_spot_interruption() {
    local node_name="$1"

    if [ -z "$node_name" ]; then
        error_log "simulate_spot_interruption requires node_name"
        return 1
    fi

    debug_log "Simulating spot interruption for node $node_name..."

    # Get the EC2 instance ID from the node's provider ID
    local provider_id
    provider_id=$(kubectl get node "$node_name" -o jsonpath='{.spec.providerID}')

    local instance_id
    instance_id=$(echo "$provider_id" | sed 's|.*/||')

    if [ -z "$instance_id" ]; then
        error_log "Failed to get instance ID for node $node_name"
        return 1
    fi

    debug_log "Terminating EC2 instance $instance_id..."

    aws ec2 terminate-instances --instance-ids "$instance_id" --region "$AWS_REGION"

    debug_log "Spot interruption simulated for instance $instance_id (node $node_name)"
}

# Get the node where a pod is running
get_pod_node() {
    local pod_name="$1"
    local namespace="${2:-$NAMESPACE}"

    kubectl get pod "$pod_name" -n "$namespace" -o jsonpath='{.spec.nodeName}' 2>/dev/null
}

# Wait for a pod to be scheduled on a Karpenter-provisioned spot node
wait_for_spot_node() {
    local pod_name="$1"
    local namespace="${2:-$NAMESPACE}"
    local timeout="${3:-300}"

    debug_log "Waiting for pod $pod_name to be scheduled on spot node (timeout: ${timeout}s)..."

    local elapsed=0
    local poll_interval=5

    while [ $elapsed -lt $timeout ]; do
        local node_name
        node_name=$(get_pod_node "$pod_name" "$namespace")

        if [ -n "$node_name" ]; then
            # Verify it's a spot instance via Karpenter label
            local capacity_type
            capacity_type=$(kubectl get node "$node_name" -o jsonpath='{.metadata.labels.karpenter\.sh/capacity-type}' 2>/dev/null)

            if [ "$capacity_type" = "spot" ]; then
                debug_log "Pod $pod_name scheduled on spot node $node_name"
                echo "$node_name"
                return 0
            elif [ -n "$capacity_type" ]; then
                debug_log "Pod scheduled on $capacity_type node, waiting for spot..."
            fi
        fi

        sleep $poll_interval
        ((elapsed += poll_interval))
    done

    error_log "Timeout waiting for pod $pod_name to be scheduled on spot node"
    return 1
}

# Create a pod spec with spot tolerations and affinity
spot_pod_spec() {
    local image="$1"
    local args="$2"
    local namespace="${3:-$NAMESPACE}"

    local name
    name=test-spot-$(unix_nano)

    local spec=/tmp/pod-${name}.yaml
    cat >"$spec" <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: "$namespace"
  labels:
    app: "$name"
    cedana.ai/spot-test: "true"
spec:
  tolerations:
    - key: "cedana.ai/spot-test"
      operator: "Equal"
      value: "true"
      effect: NoSchedule
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: karpenter.sh/capacity-type
                operator: In
                values: ["spot"]
  containers:
  - name: "$name"
    image: $image
    command: ["/bin/sh", "-c"]
    resources:
      requests:
        cpu: "500m"
        memory: "512Mi"
EOF

    if [[ -n "$args" ]]; then
        printf "    args:\n" >>"$spec"
        printf "      - |\n" >>"$spec"
        while IFS= read -r line; do
            printf "        %s\n" "$line" >>"$spec"
        done <<<"$args"
    fi

    echo "$spec"
}

# Create a GPU pod spec with spot tolerations
spot_pod_spec_gpu() {
    local image="$1"
    local args="$2"
    local gpus="${3:-1}"
    local namespace="${4:-$NAMESPACE}"

    local name
    name=test-spot-cuda-$(unix_nano)

    local spec=/tmp/pod-${name}.yaml
    cat >"$spec" <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: "$name"
  namespace: "$namespace"
  labels:
    app: "$name"
    cedana.ai/spot-test: "true"
spec:
  runtimeClassName: cedana
  tolerations:
    - key: "cedana.ai/spot-test"
      operator: "Equal"
      value: "true"
      effect: NoSchedule
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: karpenter.sh/capacity-type
                operator: In
                values: ["spot"]
  containers:
  - name: "$name"
    image: $image
    command: ["/bin/sh", "-c"]
    resources:
      limits:
        nvidia.com/gpu: "$gpus"
EOF

    if [[ -n "$args" ]]; then
        printf "    args:\n" >>"$spec"
        printf "      - |\n" >>"$spec"
        while IFS= read -r line; do
            printf "        %s\n" "$line" >>"$spec"
        done <<<"$args"
    fi

    echo "$spec"
}
