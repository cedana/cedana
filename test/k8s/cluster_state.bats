#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,cluster_state

load ../helpers/utils
load ../helpers/daemon
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator
load ../helpers/propagator_deploy

setup() {
    if ! propagator_enabled; then
        skip "cluster state sync tests need the in-cluster propagator"
    fi
}

pod_count() {
    curl -s "${CEDANA_URL}/pods/count?cluster_id=${CLUSTER_ID}${1:+&status=$1}" -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" | jq '.total_count // 0'
}

pod_status() {
    curl -s "${CEDANA_URL}/pods?id=$1" -H "Authorization: Bearer ${CEDANA_AUTH_TOKEN}" | jq -r '.pods[0].status // empty'
}

# bats test_tags=watch
@test "Cluster watch is live" {
    local state=""
    for _ in $(seq 1 60); do
        state=$(sync_status "$CLUSTER_ID" | jq -r '.watch_state // empty')
        [ "$state" = live ] && break
        sleep 2
    done
    [ "$state" = live ]
}

# bats test_tags=watch
@test "A created pod is synced and a deleted pod is tombstoned" {
    local name="state-$RANDOM$RANDOM"
    kubectl run "$name" -n "$NAMESPACE" --image=registry.k8s.io/pause:3.10 --restart=Never
    local uid
    uid=$(kubectl get pod "$name" -n "$NAMESPACE" -o jsonpath='{.metadata.uid}')

    local status=""
    for _ in $(seq 1 20); do
        status=$(pod_status "$uid")
        [ -n "$status" ] && break
        sleep 0.5
    done
    [ -n "$status" ]

    kubectl delete pod "$name" -n "$NAMESPACE" --grace-period=0 --force
    for _ in $(seq 1 20); do
        status=$(pod_status "$uid")
        [ "$status" = deleted ] && break
        sleep 0.5
    done
    [ "$status" = deleted ]
}

# bats test_tags=watch
@test "Synced pods match the cluster" {
    local expected actual
    for _ in $(seq 1 30); do
        expected=$(kubectl get pods -A -o json | jq '[.items[] | select(.metadata.deletionTimestamp == null)] | length')
        actual=$(($(pod_count) - $(pod_count deleted)))
        if [ "$actual" -ge 1 ] && [ "$actual" -ge $((expected - 5)) ] && [ "$actual" -le $((expected + 5)) ]; then
            return 0
        fi
        sleep 1
    done
    echo "propagator lists $actual pods, kubectl $expected"
    return 1
}
