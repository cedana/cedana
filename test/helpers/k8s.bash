#!/usr/bin/env bash

##########################
### Kubernetes Helpers ###
##########################

# Generate a new spec from an existing one with a new name.
new_spec () {
    local spec="$1"
    local newname="$2"

    local newspec="/tmp/${newname}.yaml"

    # Get the oldname from the first "name:" line
    local oldname
    oldname=$(grep -m1 '^[[:space:]]*name:' "$spec" | sed -E 's/^[[:space:]]*name:[[:space:]]*"?([^"]+)"?/\1/')

    # Replace all 'name: <oldname>' patterns with the quoted newname
    sed -E "s/^([[:space:]\-]*name:[[:space:]]*)\"?$oldname\"?/\1\"$newname\"/g" "$spec" > "$newspec"

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

    if ! kubectl get pod "$name" -n "$namespace" &>/dev/null; then
        debug_log "Pod $name does not exist in namespace $namespace"
        return 1
    fi

    debug_log "Waiting for pod $name in namespace $namespace to become Ready"

    if kubectl wait --for=condition=Ready pod/"$name" -n "$namespace" --timeout="$timeout" 2>/dev/null; then
        debug_log "Pod $name is Ready"
        return 0
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

tail_all_logs() {
    local namespace="$1"
    local timeout="${2:-120s}"

    wait_for_cmd "$timeout" "kubectl get pods -n $namespace | grep -q ."

    debug_log "Waiting for all pods in namespace $namespace to be Running"

    kubectl get pods -n "$namespace" -o name | xargs -n1 -P0 -I{} kubectl wait --for=jsonpath='{.status.phase}=Running' -n "$namespace" --timeout="$timeout" {}

    debug_log "Tailing all logs in namespace $namespace"

    debug "kubectl get pods -n $namespace -o name | xargs -n1 -P0 -I{} kubectl logs -n $namespace -f {}"
}
