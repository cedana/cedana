#!/usr/bin/env bash

##########################
### Kubernetes Helpers ###
##########################

# Image the GPU pods run, with the CUDA workloads at /cedana-samples/gpu_smr: a slim one, as
# customer images are (a CUDA base and the app), rather than the whole CI image
export GPU_WORKLOAD_IMAGE="${GPU_WORKLOAD_IMAGE:-cedana/cedana-samples:cuda-smr}"

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

# Checks what the kubelet gives every container of a pod, which a restore must recreate rather
# than take from the checkpoint: /etc/resolv.conf, /etc/hosts and /etc/hostname (bind mounts;
# without them the pod has no DNS, or keeps the checkpoint source's hostname), and the service
# account token. Containers whose image has no `cat` are skipped.
verify_restored_pod() {
    local name="$1"
    local namespace="${2:-$NAMESPACE}"

    local pod
    pod=$(kubectl get pod "$name" -n "$namespace" -o json) || {
        error_log "Failed to get pod $name"
        return 1
    }

    local host_network automount hostname
    host_network=$(echo "$pod" | jq -r '.spec.hostNetwork // false')
    automount=$(echo "$pod" | jq -r 'if .spec.automountServiceAccountToken == false then "false" else "true" end') # not `//`, which treats false as unset
    hostname=$(echo "$pod" | jq -r '.spec.hostname // .metadata.name')
    hostname=${hostname:0:63} # as the kubelet truncates it

    local errors=()

    # A restored container that crashed and was restarted from scratch makes the pod Ready
    # again, but it isn't the restored state anymore
    local restarted
    restarted=$(echo "$pod" | jq -r '.status.containerStatuses[]? | select(.restartCount > 0) | "\(.name) (\(.restartCount)x)"')
    if [ -n "$restarted" ]; then
        errors+=("restarted after the restore: $(echo "$restarted" | tr '\n' ' ')")
    fi

    local container
    for container in $(echo "$pod" | jq -r '.spec.containers[].name'); do
        local resolv
        if ! resolv=$(kubectl exec "$name" -n "$namespace" -c "$container" -- cat /etc/resolv.conf 2>&1); then
            if [[ "$resolv" == *"executable file not found"* ]]; then
                debug_log "Skipping checks of container $container of pod $name, which has no cat"
                continue
            fi
            errors+=("$container: cannot read /etc/resolv.conf: $resolv")
            continue
        fi
        grep -q '^nameserver ' <<<"$resolv" || errors+=("$container: /etc/resolv.conf has no nameserver (no DNS)")

        if [ "$host_network" != "true" ]; then
            local actual
            actual=$(kubectl exec "$name" -n "$namespace" -c "$container" -- cat /etc/hostname 2>&1 | tr -d '[:space:]')
            [ "$actual" = "$hostname" ] || errors+=("$container: /etc/hostname is '$actual', want '$hostname'")
            kubectl exec "$name" -n "$namespace" -c "$container" -- cat /etc/hosts 2>&1 | grep -qw -- "$hostname" ||
                errors+=("$container: /etc/hosts does not list '$hostname'")

            # The UTS namespace's hostname (what `hostname` and gethostname() return), which CRIU
            # restores from the checkpoint. Only warns for now (customer issue C1: restored pods
            # report the source pod's hostname); make it an error once restores set it.
            local uts
            uts=$(kubectl exec "$name" -n "$namespace" -c "$container" -- cat /proc/sys/kernel/hostname 2>&1 | tr -d '[:space:]')
            [ "$uts" = "$hostname" ] ||
                warn_log "$container: kernel hostname is '$uts', want '$hostname' (restored from the checkpoint?)"
        fi

        local gpus
        gpus=$(echo "$pod" | jq -r --arg c "$container" '.spec.containers[] | select(.name == $c) | .resources.limits["nvidia.com/gpu"] // empty')
        if [ -n "$gpus" ] && [ "$gpus" != "0" ]; then
            # The driver must work, and the libcuda the loader resolves must be the real one
            # (customer issue CED-2220: a stale ld.so.cache resolved another, after restore)
            local gpu_problems
            gpu_problems=$(kubectl exec "$name" -n "$namespace" -c "$container" -- sh -c '
                nvidia-smi -L >/dev/null 2>&1 || echo "nvidia-smi fails"
                lib=$(ldconfig -p 2>/dev/null | awk "/libcuda\\.so\\.1 /{print \$NF; exit}")
                if [ -z "$lib" ]; then echo "the loader cache has no libcuda.so.1"; exit; fi
                real=$(readlink -f "$lib")
                if [ ! -s "$real" ]; then echo "libcuda.so.1 ($lib) is missing or empty"; exit; fi
                [ "$(head -c 4 "$real" | tail -c 3)" = "ELF" ] || echo "libcuda.so.1 ($real) is not an ELF file"
                [ "$(wc -c <"$real")" -gt 1048576 ] || echo "libcuda.so.1 ($real) is only $(wc -c <"$real") bytes"
            ' 2>/dev/null) || gpu_problems="${gpu_problems:+$gpu_problems; }could not run the check"
            # Only warns for now: restores may not rebuild the loader cache yet (the nvidia hook
            # that runs ldconfig is removed on restore). Make it an error once they pass.
            [ -z "$gpu_problems" ] ||
                warn_log "$container: GPU: $(tr '\n' ';' <<<"$gpu_problems")"
        fi

        if [ "$automount" != "false" ]; then
            local token_namespace
            token_namespace=$(kubectl exec "$name" -n "$namespace" -c "$container" -- cat /var/run/secrets/kubernetes.io/serviceaccount/namespace 2>&1)
            [ "$token_namespace" = "$namespace" ] || errors+=("$container: service account token missing or wrong ($token_namespace)")
        fi
    done

    if [ ${#errors[@]} -gt 0 ]; then
        error_log "Restored pod $name is not what the kubelet would have created:"
        local e
        for e in "${errors[@]}"; do
            error_log "  $e"
        done
        return 1
    fi
    debug_log "Restored pod $name has its DNS, hosts, hostname and service account token"
}

# The first and last count a counting workload (e.g. count-loop.sh) logged, or nothing
# @param $1: Pod name
# @param $2: Namespace (optional, defaults to $NAMESPACE)
# @param $3: Container name (optional)
first_count() {
    kubectl logs "$1" -n "${2:-$NAMESPACE}" ${3:+-c "$3"} 2>/dev/null | grep -m1 -oE '^COUNT [0-9]+' | awk '{print $2}'
}

last_count() {
    kubectl logs "$1" -n "${2:-$NAMESPACE}" ${3:+-c "$3"} 2>/dev/null | grep -oE '^COUNT [0-9]+' | tail -n 1 | awk '{print $2}'
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

    # Get all pods excluding Completed pods
    local pods
    pods=$(kubectl get pods -n "$namespace" -o json 2>/dev/null | jq -r '.items[] | select(.status.phase != "Succeeded") | "pod/" + .metadata.name')

    if [ -z "$pods" ]; then
        debug_log "No Running pods found in namespace $namespace"
        return 0
    fi

    local end=$((SECONDS + timeout))
    local bad_pods
    # Check if all pods are in Completed/Ready state
    while ((SECONDS < end)); do
        bad_pods=$(kubectl get pods -n "$namespace" -o json | jq -r '
        .items[]
        | select(
            .status.phase != "Succeeded"
            and (
                .status.phase != "Running"
                or (
                    (.status.conditions[]? | select(.type=="Ready").status) != "True"
                )
            )
        )
        | .metadata.name
    ')

        if [ -z "$bad_pods" ]; then
            debug_log "All pods in namespace $namespace are Ready"
            return 0
        fi

        sleep 2
    done
    error_log "Failed to wait for all pods in namespace $namespace to be Ready"
    for pod in $(kubectl get pods -n "$namespace" -o name); do
        local phase
        phase=$(kubectl get "$pod" -n "$namespace" -o jsonpath='{.status.phase}')
        if [ "$phase" != "Succeeded" ]; then
            error_log "Pod $pod status: $phase"
            kubectl describe "$pod" -n "$namespace" | awk '/^Events:/,0' | while read -r line; do
                error_log "$line"
            done
            error_log "Logs from pod $pod in namespace $namespace:"
            error kubectl logs "$pod" -n "$namespace" --tail=1000 --
        fi
    done
    return 1
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

# Deletes pods in a namespace and waits for them to go away, force deleting
# (--grace-period=0 --force) any pod still Terminating once the timeout expires,
# so a container the kubelet cannot kill never blocks the suite. Dumps the
# description of each stuck pod before force deleting it.
# @param $1: Namespace
# @param $2: Seconds to wait for graceful deletion (default: 60)
# @param $3: Label selector (default: all pods in the namespace)
delete_pods_or_force() {
    local namespace="$1"
    local timeout="${2:-60}"
    local selector="${3:-}"

    local select=(--all)
    if [ -n "$selector" ]; then
        select=(-l "$selector")
    fi

    kubectl delete pod -n "$namespace" "${select[@]}" --wait=false --ignore-not-found 2>/dev/null || true
    kubectl wait --for=delete pod -n "$namespace" "${select[@]}" --timeout="${timeout}s" 2>/dev/null || true

    # kubectl get has no --all flag; listing without -l already returns every pod.
    local list=()
    if [ -n "$selector" ]; then
        list=(-l "$selector")
    fi

    # A failed listing must not pass for an empty one: fall through and force delete.
    local stuck
    if stuck=$(kubectl get pod -n "$namespace" "${list[@]}" -o name 2>/dev/null); then
        if [ -z "$stuck" ]; then
            return 0
        fi
    else
        error_log "Failed to list pods in namespace $namespace, force deleting anyway"
    fi

    local pod
    for pod in $stuck; do
        error_log "$pod in namespace $namespace still exists after ${timeout}s, force deleting"
        error kubectl describe "$pod" -n "$namespace" || true
    done
    kubectl delete pod -n "$namespace" "${select[@]}" --grace-period=0 --force --ignore-not-found --timeout="${timeout}s" || {
        error_log "Failed to force delete pods in namespace $namespace"
        return 1
    }
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
# @param $5: Dump timeout (optional, defaults to 60)
# @param $6: Namespace (optional, defaults to $NAMESPACE)
# @param $7: Dump trigger string (optional) - if set, waits for this string in logs instead of using dump_wait_time
# @param $8: Trigger timeout in seconds (optional, defaults to 300) - only used if dump_trigger is set
# @param $9: Post-trigger wait time in seconds (optional, defaults to 0) - additional wait after trigger is found
test_pod_spec() {
    local action_sequence="$1"
    local spec="$2"
    local pod_timeout="${3:-60}"
    local dump_wait_time="${4:-5}"
    local dump_timeout="${5:-60}"
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
    local count_before_dump="" # for workloads that log "COUNT <n>"

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

                # At most the count the checkpoint captures
                count_before_dump=$(last_count "$name" "$namespace" "$container_name")

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

                name=$(wait_for_cmd 60 get_restored_pod "$original_name" "$namespace") || {
                    error="Restored pod for $original_name did not appear"
                    name=""
                    break
                }

                debug_log "Restore starting for $name..."

                validate_pod "$name" "$pod_timeout" || {
                    error="Restored pod $name failed to become Ready"
                    break
                }

                # Ready alone doesn't mean it restored: it may have cold-started instead
                poll_restore_status "$restore_action_id" "$pod_timeout" || {
                    error="Pod $name did not restore (restore $restore_action_id)"
                    break
                }

                verify_restored_pod "$name" "$namespace" || {
                    error="Restored pod $name is missing what the kubelet gives it"
                    break
                }

                # The workload continued from the checkpoint, rather than starting over
                if [ -n "$count_before_dump" ]; then
                    wait_for_cmd 30 "kubectl logs $name -n $namespace -c $container_name | grep -qE '^COUNT [0-9]+'" || {
                        error="Restored pod $name logs no count"
                        break
                    }
                    local count_after_restore
                    count_after_restore=$(first_count "$name" "$namespace" "$container_name")
                    if [ -z "$count_after_restore" ] || [ "$count_after_restore" -lt "$count_before_dump" ]; then
                        error="Restored pod $name started at count $count_after_restore, but it was at $count_before_dump before the checkpoint"
                        break
                    fi
                    debug_log "Restored pod $name continued at count $count_after_restore (was $count_before_dump)"
                fi

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
    if [ -n "$error" ] && [ -n "$name" ]; then
        debug_log "Fetching logs from pod $name..."
        error kubectl logs "$name" -n "$namespace" --tail=500 || true
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

    while [ $elapsed -lt "$timeout" ]; do
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

# Collects what's needed to debug a failed run into a directory, for CI to upload: the
# cedana daemon log, containerd logs (the shims log through containerd), the logs of all
# pods in the Cedana namespace (incl. previous containers), pods and events of the cluster,
# and the propagator's recent restores for this cluster (with a summary by status). Best
# effort: every step is bounded, and it never fails. Call it before anything is torn down.
# @param $1: Output directory (default: /tmp/k8s-diagnostics)
# @param $2: Max bytes per log (default: 20M, as for tail -c)
collect_diagnostics() {
    local dir="${1:-/tmp/k8s-diagnostics}"
    local max="${2:-20M}"
    (
        set +e +o pipefail
        rm -rf "$dir" 2>/dev/null
        mkdir -p "$dir" || exit 0
        local kc=(kubectl --request-timeout=30s)
        local ns="${CEDANA_NAMESPACE:-cedana-system}"

        debug_log "Collecting diagnostics into $dir..."

        # Cluster state
        "${kc[@]}" get nodes -o wide > "$dir/nodes.txt" 2>&1
        "${kc[@]}" get pods -A -o wide > "$dir/pods.txt" 2>&1
        "${kc[@]}" get events -A --sort-by=.lastTimestamp > "$dir/events.txt" 2>&1
        "${kc[@]}" describe pods -n "$ns" > "$dir/describe-$ns.txt" 2>&1

        # Logs of all pods in the Cedana namespace, and of their previous containers if restarted
        local pod restarts
        mkdir -p "$dir/pods"
        while read -r pod restarts; do
            [ -n "$pod" ] || continue
            timeout 120 "${kc[@]}" logs -n "$ns" "$pod" --all-containers --prefix --timestamps 2>&1 |
                tail -c "$max" > "$dir/pods/$pod.log"
            if [[ "$restarts" =~ [1-9] ]]; then # any container restarted
                timeout 120 "${kc[@]}" logs -n "$ns" "$pod" --all-containers --prefix --timestamps --previous 2>&1 |
                    tail -c "$max" > "$dir/pods/$pod.previous.log"
            fi
        done < <("${kc[@]}" get pods -n "$ns" \
                -o jsonpath='{range .items[*]}{.metadata.name}{" "}{.status.containerStatuses[*].restartCount}{"\n"}{end}' 2>/dev/null)

        # Node logs, through each helper pod (which mounts the node's root at /host), so it
        # works on any platform: the daemon log (scripts/install-service.sh logs to it, also
        # under systemd), and the daemon's and containerd's journals where there's systemd
        local helper node
        mkdir -p "$dir/nodes"
        while read -r helper node; do
            [ -n "$helper" ] || continue
            node="${node:-$helper}"
            timeout 120 "${kc[@]}" exec -n "$ns" "$helper" -c helper -- \
                sh -c "tail -c $max /host/var/log/cedana-daemon.log" > "$dir/nodes/$node.cedana-daemon.log" 2>&1
            timeout 120 "${kc[@]}" exec -n "$ns" "$helper" -c helper -- \
                sh -c "chroot /host journalctl -u cedana -u containerd -u k3s -u k3s-agent --no-pager -o short-precise 2>/dev/null | tail -c $max" \
                > "$dir/nodes/$node.journal.log" 2>&1
        done < <("${kc[@]}" get pods -n "$ns" -l app.kubernetes.io/component=helper \
                -o go-template='{{range .items}}{{.metadata.name}} {{.spec.nodeName}}{{"\n"}}{{end}}' 2>/dev/null)

        # Local node logs, for K3s, whose node is this machine (and if the helper is gone)
        mkdir -p "$dir/local"
        local f
        for f in /var/log/cedana*.log "${CONTAINERD_LOG:-/tmp/containerd.log}" \
            /var/lib/rancher/k3s/agent/containerd/containerd.log; do
            [ -f "$f" ] && tail -c "$max" "$f" > "$dir/local/$(basename "$f")"
        done
        if command -v journalctl &>/dev/null; then
            timeout 60 journalctl -u cedana -u containerd -u k3s --no-pager -o short-precise 2>/dev/null |
                tail -c "$max" > "$dir/local/journal.log"
        fi

        # Recent restores of this cluster from the propagator, with a summary by status
        if [ -n "${CEDANA_URL:-}" ]; then
            local query="limit=50"
            [ -n "${CLUSTER_ID:-}" ] && query+="&cluster_id=${CLUSTER_ID//\"/}"
            curl -s --max-time 30 "${CEDANA_URL}/restores?${query}" \
                -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN:-}" 2>/dev/null |
                head -c 50M > "$dir/restores.json"
            local summary
            summary=$(jq -r 'if type == "array" then
                    (group_by(.status) | map("\(.[0].status // "unknown")=\(length)") | join(" ")) as $s
                    | "\(length) restores\(if $s != "" then ": " + $s else "" end)"
                else empty end' "$dir/restores.json" 2>/dev/null)
            if [ -n "$summary" ]; then
                echo "$summary" > "$dir/restores-summary.txt"
                info_log "Propagator restores for cluster ${CLUSTER_ID:-?} (last 50): $summary"
            else
                info_log "Propagator restores for cluster ${CLUSTER_ID:-?}: unavailable"
            fi
        fi

        find "$dir" -type f -empty -delete 2>/dev/null
        info_log "Diagnostics collected in $dir"
        exit 0
    ) || true
}
