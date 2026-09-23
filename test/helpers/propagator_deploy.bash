#!/usr/bin/env bash

###########################################
### In-cluster Cedana Propagator Helpers ###
###########################################
#
# Deploys a throwaway propagator (with Postgres, RabbitMQ and ClickHouse) into the
# test cluster, so tests can run against a specific propagator build instead of the
# shared CEDANA_URL. Enabled by setting PROPAGATOR_REPO with PROPAGATOR_DIGEST or
# PROPAGATOR_TAG.
#
# The propagator runs with TEST_MODE, so it accepts a single per-run token instead
# of PropelAuth. After deploy_propagator:
#   CEDANA_URL          - propagator URL reachable from the test runner (port-forward)
#   CEDANA_CLUSTER_URL  - propagator URL reachable from inside the cluster (ClusterIP)
#   CEDANA_AUTH_TOKEN   - the per-run token
#
# ClusterIPs are used instead of service DNS because the cedana daemon runs on the
# host, where cluster DNS does not resolve.
#
# Environment variables:
#   PROPAGATOR_REPO                 - Propagator image repository
#   PROPAGATOR_DIGEST               - Propagator image digest (preferred over tag)
#   PROPAGATOR_TAG                  - Propagator image tag
#   PROPAGATOR_NAMESPACE            - Namespace for the propagator stack (default: cedana-propagator)
#   PROPAGATOR_BUCKET_NAME          - Bucket for checkpoint files (default: cedana-checkpoints-storage)
#   PROPAGATOR_PLUGINS_BUCKET       - S3 bucket plugins are served from (default: cedana-bin)
#   PROPAGATOR_LOG_LEVEL            - RUST_LOG for the propagator (default: info)
#   DOCKER_USERNAME, DOCKER_TOKEN   - If set, used as the image pull secret
#   AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_REGION - S3 access (plugins, metrics)
#   GCLOUD_SERVICE_ACCOUNT_KEY      - If set, GCS access (checkpoint files)

export PROPAGATOR_NAMESPACE="${PROPAGATOR_NAMESPACE:-cedana-propagator}"
PROPAGATOR_PORT=1324
PROPAGATOR_LOG_FILE="${PROPAGATOR_LOG_FILE:-/tmp/propagator.log}"
PROPAGATOR_PORT_FORWARD_LOG="/tmp/propagator-port-forward.log"
export PROPAGATOR_PORT_FORWARD_PID=""

propagator_enabled() {
    [ -n "${PROPAGATOR_REPO:-}" ] && { [ -n "${PROPAGATOR_DIGEST:-}" ] || [ -n "${PROPAGATOR_TAG:-}" ]; }
}

propagator_image() {
    if [ -n "${PROPAGATOR_DIGEST:-}" ]; then
        echo "$PROPAGATOR_REPO@$PROPAGATOR_DIGEST"
    else
        echo "$PROPAGATOR_REPO:$PROPAGATOR_TAG"
    fi
}

random_token() {
    head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
}

cluster_ip() {
    kubectl get svc "$1" -n "$PROPAGATOR_NAMESPACE" -o jsonpath='{.spec.clusterIP}'
}

deploy_propagator() {
    local ns="$PROPAGATOR_NAMESPACE"
    local image
    image=$(propagator_image)

    debug_log "Deploying propagator $image into namespace $ns..."

    delete_namespace "$ns"
    create_namespace "$ns"

    local token db_password mq_password
    token=$(random_token)
    db_password=$(random_token)
    mq_password=$(random_token)

    local pull_secrets="[]"
    if [ -n "${DOCKER_USERNAME:-}" ] && [ -n "${DOCKER_TOKEN:-}" ]; then
        kubectl create secret docker-registry propagator-pull -n "$ns" \
            --docker-username="$DOCKER_USERNAME" \
            --docker-password="$DOCKER_TOKEN" >/dev/null
        pull_secrets='[{"name": "propagator-pull"}]'
    fi

    kubectl create secret generic propagator-env -n "$ns" \
        --from-literal=CEDANA_AUTH_TOKEN="$token" \
        --from-literal=POSTGRES_PASSWORD="$db_password" \
        --from-literal=RABBITMQ_PASSWORD="$mq_password" \
        --from-literal=POSTGRES_DB_URI="postgresql://cedana:$db_password@postgres:5432/cedana" \
        --from-literal=DATABASE_URL="postgresql://cedana:$db_password@postgres:5432/cedana" \
        --from-literal=RABBITMQ_URI="amqp://cedana:$mq_password@rabbitmq:5672" \
        --from-literal=AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-}" \
        --from-literal=AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-}" >/dev/null

    local gcp_env="" gcp_mount="" gcp_volume=""
    if [ -n "${GCLOUD_SERVICE_ACCOUNT_KEY:-}" ]; then
        kubectl create secret generic propagator-gcp -n "$ns" \
            --from-literal=key.json="$GCLOUD_SERVICE_ACCOUNT_KEY" >/dev/null
        gcp_env='
            - name: GOOGLE_APPLICATION_CREDENTIALS
              value: /var/secrets/gcp/key.json'
        gcp_mount='
            - name: gcp
              mountPath: /var/secrets/gcp
              readOnly: true'
        gcp_volume='
        - name: gcp
          secret:
            secretName: propagator-gcp'
    fi

    kubectl apply -n "$ns" -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgres
spec:
  selector:
    matchLabels: { app: postgres }
  template:
    metadata:
      labels: { app: postgres }
    spec:
      containers:
        - name: postgres
          image: postgres:17
          env:
            - { name: POSTGRES_USER, value: cedana }
            - { name: POSTGRES_DB, value: cedana }
            - name: POSTGRES_PASSWORD
              valueFrom: { secretKeyRef: { name: propagator-env, key: POSTGRES_PASSWORD } }
          ports: [{ containerPort: 5432 }]
          readinessProbe:
            exec: { command: [pg_isready, -U, cedana, -d, cedana] }
            periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
spec:
  selector: { app: postgres }
  ports: [{ port: 5432 }]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rabbitmq
spec:
  selector:
    matchLabels: { app: rabbitmq }
  template:
    metadata:
      labels: { app: rabbitmq }
    spec:
      containers:
        - name: rabbitmq
          image: rabbitmq:3-management
          env:
            - { name: RABBITMQ_DEFAULT_USER, value: cedana }
            - name: RABBITMQ_DEFAULT_PASS
              valueFrom: { secretKeyRef: { name: propagator-env, key: RABBITMQ_PASSWORD } }
          ports: [{ containerPort: 5672 }]
          readinessProbe:
            exec: { command: [rabbitmq-diagnostics, -q, ping] }
            periodSeconds: 5
            timeoutSeconds: 10
---
apiVersion: v1
kind: Service
metadata:
  name: rabbitmq
spec:
  selector: { app: rabbitmq }
  ports: [{ port: 5672 }]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: clickhouse
spec:
  selector:
    matchLabels: { app: clickhouse }
  template:
    metadata:
      labels: { app: clickhouse }
    spec:
      containers:
        - name: clickhouse
          image: clickhouse/clickhouse-server:24.8
          env:
            - { name: CLICKHOUSE_DB, value: cedana }
            - { name: CLICKHOUSE_SKIP_USER_SETUP, value: "1" }
          ports: [{ containerPort: 8123 }]
          readinessProbe:
            httpGet: { path: /ping, port: 8123 }
            periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: clickhouse
spec:
  selector: { app: clickhouse }
  ports: [{ port: 8123 }]
---
apiVersion: v1
kind: Service
metadata:
  name: cedana-propagator
spec:
  selector: { app: cedana-propagator }
  ports: [{ port: $PROPAGATOR_PORT }]
EOF

    local dep
    for dep in postgres rabbitmq clickhouse; do
        kubectl rollout status deployment/"$dep" -n "$ns" --timeout=5m || {
            error_log "Propagator dependency $dep failed to become ready"
            error kubectl describe pods -n "$ns" -l app="$dep"
            return 1
        }
    done

    # Handed out to daemons via service discovery, so must be reachable from the host
    local mq_discovery_uri
    mq_discovery_uri="amqp://cedana:$mq_password@$(cluster_ip rabbitmq):5672"

    kubectl apply -n "$ns" -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cedana-propagator
spec:
  selector:
    matchLabels: { app: cedana-propagator }
  template:
    metadata:
      labels: { app: cedana-propagator }
    spec:
      imagePullSecrets: $pull_secrets
      containers:
        - name: cedana-propagator
          image: $image
          envFrom:
            - secretRef: { name: propagator-env }
          env:
            - { name: TEST_MODE, value: "true" }
            - { name: RUST_LOG, value: "${PROPAGATOR_LOG_LEVEL:-info}" }
            - { name: BUCKET_NAME, value: "${PROPAGATOR_BUCKET_NAME:-cedana-checkpoints-storage}" }
            - { name: PLUGINS_BUCKET, value: "${PROPAGATOR_PLUGINS_BUCKET:-cedana-bin}" }
            - { name: AWS_REGION, value: "${AWS_REGION:-us-east-1}" }
            - { name: RABBITMQ_DISCOVERY_URI, value: "$mq_discovery_uri" }
            - { name: CLICKHOUSE_URL, value: "http://clickhouse:8123" }
            - { name: CLICKHOUSE_DATABASE, value: cedana }
            - { name: CLICKHOUSE_USER, value: default }
            - { name: CLICKHOUSE_PASSWORD, value: "" }$gcp_env
          ports: [{ containerPort: $PROPAGATOR_PORT }]
          readinessProbe:
            tcpSocket: { port: $PROPAGATOR_PORT }
            periodSeconds: 5
          volumeMounts:
            - { name: duckdb, mountPath: /duckdb }$gcp_mount
      volumes:
        - name: duckdb
          emptyDir: {}$gcp_volume
EOF

    kubectl rollout status deployment/cedana-propagator -n "$ns" --timeout=5m || {
        error_log "Propagator failed to become ready"
        error kubectl describe pods -n "$ns" -l app=cedana-propagator
        error kubectl logs -n "$ns" deployment/cedana-propagator --tail=1000
        return 1
    }

    local local_port
    local_port=$(random_free_port)

    # Restart the port-forward if it drops (e.g. on propagator restart). fd 3 is closed
    # so bats does not wait on this background process.
    (
        while true; do
            kubectl port-forward -n "$ns" svc/cedana-propagator "$local_port:$PROPAGATOR_PORT" || true
            sleep 1
        done
    ) >"$PROPAGATOR_PORT_FORWARD_LOG" 2>&1 3>&- &
    PROPAGATOR_PORT_FORWARD_PID=$!

    export CEDANA_URL="http://127.0.0.1:$local_port/v1"
    export CEDANA_CLUSTER_URL="http://$(cluster_ip cedana-propagator):$PROPAGATOR_PORT/v1"
    export CEDANA_AUTH_TOKEN="$token"
    PROPAGATOR_BASE_URL="$CEDANA_URL"
    PROPAGATOR_AUTH_TOKEN="$CEDANA_AUTH_TOKEN"

    wait_for_cmd 120 "curl -sf -o /dev/null -H 'Authorization: Bearer $token' $CEDANA_URL/user" || {
        error_log "Propagator is not reachable at $CEDANA_URL"
        error cat "$PROPAGATOR_PORT_FORWARD_LOG"
        error kubectl logs -n "$ns" deployment/cedana-propagator --tail=1000
        return 1
    }

    info_log "Propagator $image deployed (runner: $CEDANA_URL, cluster: $CEDANA_CLUSTER_URL)"
}

teardown_propagator() {
    if [ -n "$PROPAGATOR_PORT_FORWARD_PID" ]; then
        pkill -P "$PROPAGATOR_PORT_FORWARD_PID" 2>/dev/null || true
        kill "$PROPAGATOR_PORT_FORWARD_PID" 2>/dev/null || true
        PROPAGATOR_PORT_FORWARD_PID=""
    fi

    if kubectl get namespace "$PROPAGATOR_NAMESPACE" &>/dev/null; then
        kubectl logs -n "$PROPAGATOR_NAMESPACE" deployment/cedana-propagator --tail=-1 \
            >"$PROPAGATOR_LOG_FILE" 2>&1 || true
        delete_namespace "$PROPAGATOR_NAMESPACE" --wait=false
    fi
}
