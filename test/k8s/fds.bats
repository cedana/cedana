#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,fds

load ../helpers/utils
load ../helpers/daemon
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

# Dumps and restores a pod running a cedana-samples:cpu sample (path under /app/cpu_smr, then
# its args), which exits on any lost or corrupted state: it isn't restarted from scratch, so
# the restored pod fails then. With PERSISTENT_MOUNTS set, the container gets it as
# CEDANA_PERSISTENT_MOUNTS, under the cedana runtime class, which applies it on creation.
restore_sample_pod() {
    local sample="/app/cpu_smr/$1"
    shift
    local spec
    spec=$(cmd_pod_spec "cedana/cedana-samples:cpu" "if [ ! -x $sample ]; then echo NO_SAMPLE; exec sleep infinity; fi
exec python3 $sample $*")
    sed -i 's/^spec:$/spec:\n  restartPolicy: Never/' "$spec"
    if [ -n "${PERSISTENT_MOUNTS:-}" ]; then
        sed -i 's/^spec:$/spec:\n  runtimeClassName: cedana/' "$spec"
        printf '    env:\n    - name: CEDANA_PERSISTENT_MOUNTS\n      value: "%s"\n' "$PERSISTENT_MOUNTS" >>"$spec"
    fi

    local name
    name=$(get_created_pod "$spec" "$NAMESPACE" 30)
    kubectl apply -f "$spec"
    validate_pod "$name" 300
    wait_for_cmd 60 "kubectl logs $name -n $NAMESPACE | grep -qE '^(OK|NO_SAMPLE)'"
    if kubectl logs "$name" -n "$NAMESPACE" | grep -q NO_SAMPLE; then
        kubectl delete pod "$name" -n "$NAMESPACE" --wait=false
        skip "$sample is not in cedana/cedana-samples:cpu yet"
    fi
    kubectl delete pod "$name" -n "$NAMESPACE" --wait=true

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 300 5 120
}

# A pod holding the fd kinds servers use, each with state in flight (see fd_zoo.py): restored
# into a new pod, with a new network namespace and IP, all of it must come back.
# XXX: without inotify, as CRIU can't dump a watch on the container's overlayfs: it can't open
# overlayfs file handles (no nfs_export), and its fallback scan doesn't look in the rootfs
# ("fsnotify: Can't dump that handle"). Without udp, as CRIU doesn't dump UDP receive queues
# (datagrams UDP may drop anyway).
# Without timerfd, as CRIU restores a periodic timerfd disarmed if a tick was unread at dump
# (fdinfo's it_value is 0 then, and timerfd_settime with it_value 0 disarms it).
# bats test_tags=dump,restore
@test "Restore a pod holding many kinds of fds, with state in flight" {
    restore_sample_pod fd_zoo.py --skip inotify,udp,timerfd
}

# The shape of KV-cache offload in a pod: processes sharing unlinked /dev/shm files (the
# pod's own /dev/shm, an external mount on dump, so CRIU dumps them as ghost files), a memfd
# and an unlinked file, all with zero pages and holes (see hostmem.py). A mapping no longer
# shared fails it too.
# bats test_tags=dump,restore,shmem
@test "Restore a pod sharing /dev/shm, memfd and unlinked files between processes" {
    restore_sample_pod hostmem/hostmem.py --regions ghost-shm:32M,memfd:64M,ghost-file:64M --procs 3 --dir /tmp
}

# A file still linked in /dev/shm, as multiprocessing.shared_memory makes, comes back only if
# /dev/shm is in the checkpoint: the restored pod's own is new and empty. CEDANA_PERSISTENT_MOUNTS
# makes it a tmpfs of the container's, which CRIU dumps with its contents.
# bats test_tags=dump,restore,shmem
@test "Restore a pod sharing linked /dev/shm files (CEDANA_PERSISTENT_MOUNTS)" {
    PERSISTENT_MOUNTS=/dev/shm restore_sample_pod hostmem/hostmem.py --regions shm:16M,ghost-shm:32M --procs 3 --dir /tmp
}
