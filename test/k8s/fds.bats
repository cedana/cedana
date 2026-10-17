#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,fds

load ../helpers/utils
load ../helpers/daemon
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

# A pod holding the fd kinds servers use, each with state in flight (see cedana-samples
# fd_zoo.py): restored into a new pod, with a new network namespace and IP, all of it must
# come back. fd_zoo exits on any lost or corrupted state, so the restored pod must stay
# Ready and keep making progress.
# bats test_tags=dump,restore
@test "Restore a pod holding many kinds of fds, with state in flight" {
    local script spec
    script='if [ ! -x /app/cpu_smr/fd_zoo.py ]; then echo NO_SAMPLE; exec sleep infinity; fi
exec python3 /app/cpu_smr/fd_zoo.py'
    spec=$(cmd_pod_spec "cedana/cedana-samples:cpu" "$script")
    # A sample exits when restored state is wrong: not restarted from scratch, the pod fails
    sed -i 's/^spec:$/spec:\n  restartPolicy: Never/' "$spec"

    local name
    name=$(get_created_pod "$spec" "$NAMESPACE" 30)
    kubectl apply -f "$spec"
    validate_pod "$name" 300
    wait_for_cmd 60 "kubectl logs $name -n $NAMESPACE | grep -qE '^(OK|NO_SAMPLE)'"
    if kubectl logs "$name" -n "$NAMESPACE" | grep -q NO_SAMPLE; then
        kubectl delete pod "$name" -n "$NAMESPACE" --wait=false
        skip "fd_zoo.py is not in cedana/cedana-samples:cpu yet"
    fi
    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 300 5 120
}

# The shape of KV-cache offload in a pod: processes sharing unlinked /dev/shm files (the
# pod's own /dev/shm, an external mount on dump), a memfd and an unlinked file, all with
# zero pages and holes (see cedana-samples hostmem.py). hostmem exits on any lost or corrupted
# page, or a mapping no longer shared, so the restored pod must stay Ready.
# bats test_tags=dump,restore,shmem
@test "Restore a pod sharing /dev/shm, memfd and unlinked files between processes" {
    local script spec
    script='if [ ! -x /app/cpu_smr/hostmem/hostmem.py ]; then echo NO_SAMPLE; exec sleep infinity; fi
exec python3 /app/cpu_smr/hostmem/hostmem.py --regions ghost-shm:32M,shm:16M,memfd:64M,ghost-file:64M --procs 3 --dir /tmp'
    spec=$(cmd_pod_spec "cedana/cedana-samples:cpu" "$script")
    # A sample exits when restored state is wrong: not restarted from scratch, the pod fails
    sed -i 's/^spec:$/spec:\n  restartPolicy: Never/' "$spec"

    local name
    name=$(get_created_pod "$spec" "$NAMESPACE" 30)
    kubectl apply -f "$spec"
    validate_pod "$name" 300
    wait_for_cmd 60 "kubectl logs $name -n $NAMESPACE | grep -qE '^(OK|NO_SAMPLE)'"
    if kubectl logs "$name" -n "$NAMESPACE" | grep -q NO_SAMPLE; then
        kubectl delete pod "$name" -n "$NAMESPACE" --wait=false
        skip "hostmem.py is not in cedana/cedana-samples:cpu yet"
    fi
    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 300 5 120
}
