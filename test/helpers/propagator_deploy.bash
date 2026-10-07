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
# of PropelAuth. deploy_propagator overrides CEDANA_URL and CEDANA_AUTH_TOKEN to point
# at it, so everything downstream (helm chart, daemon, test helpers) just uses those.
#
# CEDANA_URL is the propagator's ClusterIP (not service DNS, because the cedana daemon
# runs on the host where cluster DNS does not resolve). On K3s the cluster runs inside
# the test container, so that is directly reachable. On other providers the runner is
# outside the cluster network, so the ClusterIP is aliased on loopback and port-forwarded,
# making the same URL work from the runner too.
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
#   AWS_SESSION_TOKEN               - Passed along if set (temporary/SSO credentials)

export PROPAGATOR_NAMESPACE="${PROPAGATOR_NAMESPACE:-cedana-propagator}"
PROPAGATOR_PORT=1324
PROPAGATOR_LOG_FILE="${PROPAGATOR_LOG_FILE:-/tmp/propagator.log}"
PROPAGATOR_PORT_FORWARD_LOG="/tmp/propagator-port-forward.log"
export PROPAGATOR_PORT_FORWARD_PID=""
export PROPAGATOR_IP=""

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

# Also written next to PROPAGATOR_LOG_FILE so CI's propagator-logs artifact has it even
# when the suite dies in setup_suite (bats' junit formatter drops that output).
propagator_diagnostics() {
    local ns="$PROPAGATOR_NAMESPACE" out="${PROPAGATOR_LOG_FILE%.log}-deploy.log"
    {
        kubectl get pods -n "$ns" -o wide
        kubectl get events -n "$ns" --sort-by=.lastTimestamp
        kubectl describe pods -n "$ns"
        # Current and previous container logs of every pod, so a crash-looping
        # dependency explains itself too
        local pod
        for pod in $(kubectl get pods -n "$ns" -o name 2>/dev/null); do
            echo "===== logs $pod"
            kubectl logs -n "$ns" "$pod" --all-containers --tail=300
            echo "===== previous logs $pod"
            kubectl logs -n "$ns" "$pod" --all-containers --previous --tail=300 2>&1 | grep -v 'previous terminated container .* not found'
        done
    } >"$out" 2>&1
    error cat "$out"
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
        --from-literal=POSTGRES_DB_URI="postgresql://cedana:$db_password@cedana-postgres:5432/cedana" \
        --from-literal=DATABASE_URL="postgresql://cedana:$db_password@cedana-postgres:5432/cedana" \
        --from-literal=RABBITMQ_URI="amqp://cedana:$mq_password@cedana-rabbitmq:5672" \
        --from-literal=AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-}" \
        --from-literal=AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-}" \
        ${AWS_SESSION_TOKEN:+--from-literal=AWS_SESSION_TOKEN="$AWS_SESSION_TOKEN"} >/dev/null

    if [ -z "${AWS_ACCESS_KEY_ID:-}" ] || [ -z "${AWS_SECRET_ACCESS_KEY:-}" ]; then
        warn_log "AWS credentials not set: the propagator cannot serve plugins from S3, so the helper will fail to install them unless CEDANA_PLUGINS_BUILDS=local"
    fi

    kubectl apply -n "$ns" -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cedana-postgres
spec:
  selector:
    matchLabels: { app: cedana-postgres }
  template:
    metadata:
      labels: { app: cedana-postgres }
    spec:
      imagePullSecrets: $pull_secrets
      containers:
        - name: cedana-postgres
          image: postgres:17-alpine
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
  name: cedana-postgres
spec:
  selector: { app: cedana-postgres }
  ports: [{ port: 5432 }]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cedana-rabbitmq
spec:
  selector:
    matchLabels: { app: cedana-rabbitmq }
  template:
    metadata:
      labels: { app: cedana-rabbitmq }
    spec:
      imagePullSecrets: $pull_secrets
      containers:
        - name: cedana-rabbitmq
          image: rabbitmq:3-alpine # management plugin is unused; AMQP only
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
  name: cedana-rabbitmq
spec:
  selector: { app: cedana-rabbitmq }
  ports: [{ port: 5672 }]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cedana-clickhouse
spec:
  selector:
    matchLabels: { app: cedana-clickhouse }
  template:
    metadata:
      labels: { app: cedana-clickhouse }
    spec:
      imagePullSecrets: $pull_secrets
      containers:
        - name: cedana-clickhouse
          image: clickhouse/clickhouse-server:24.8-alpine
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
  name: cedana-clickhouse
spec:
  selector: { app: cedana-clickhouse }
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

    # The first wait also covers all three image pulls on small CI runners
    local dep
    for dep in cedana-postgres cedana-rabbitmq cedana-clickhouse; do
        kubectl rollout status deployment/"$dep" -n "$ns" --timeout=10m || {
            error_log "Propagator dependency $dep failed to become ready"
            propagator_diagnostics
            return 1
        }
    done

    # Handed out to daemons via service discovery, so must be reachable from the host
    local mq_discovery_uri
    mq_discovery_uri="amqp://cedana:$mq_password@$(cluster_ip cedana-rabbitmq):5672"

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
            - { name: CLICKHOUSE_URL, value: "http://cedana-clickhouse:8123" }
            - { name: CLICKHOUSE_DATABASE, value: cedana }
            - { name: CLICKHOUSE_USER, value: default }
            - { name: CLICKHOUSE_PASSWORD, value: "" }
          ports: [{ containerPort: $PROPAGATOR_PORT }]
          readinessProbe:
            tcpSocket: { port: $PROPAGATOR_PORT }
            periodSeconds: 5
          volumeMounts:
            - { name: duckdb, mountPath: /duckdb }
      volumes:
        - name: duckdb
          emptyDir: {}
EOF

    kubectl rollout status deployment/cedana-propagator -n "$ns" --timeout=5m || {
        error_log "Propagator failed to become ready"
        propagator_diagnostics
        return 1
    }

    PROPAGATOR_IP=$(cluster_ip cedana-propagator)

    if [ "$PROVIDER" != "k3s" ]; then
        # Needs CAP_NET_ADMIN; outside the test container the suite runs as a regular user
        ip addr add "$PROPAGATOR_IP/32" dev lo 2>/dev/null || sudo -n ip addr add "$PROPAGATOR_IP/32" dev lo || {
            error_log "Failed to alias $PROPAGATOR_IP on loopback (needs root or passwordless sudo)"
            return 1
        }
        # Restart the port-forward if it drops (e.g. on propagator restart). fd 3 is closed
        # so bats does not wait on this background process.
        (
            while true; do
                kubectl port-forward --address "$PROPAGATOR_IP" -n "$ns" svc/cedana-propagator "$PROPAGATOR_PORT:$PROPAGATOR_PORT" || true
                sleep 1
            done
        ) >"$PROPAGATOR_PORT_FORWARD_LOG" 2>&1 3>&- &
        PROPAGATOR_PORT_FORWARD_PID=$!
    fi

    export CEDANA_URL="http://$PROPAGATOR_IP:$PROPAGATOR_PORT/v1"
    export CEDANA_AUTH_TOKEN="$token"

    wait_for_cmd 120 "curl -sf -o /dev/null -H 'Authorization: Bearer $token' $CEDANA_URL/user" || {
        error_log "Propagator is not reachable at $CEDANA_URL"
        [ -f "$PROPAGATOR_PORT_FORWARD_LOG" ] && error cat "$PROPAGATOR_PORT_FORWARD_LOG"
        propagator_diagnostics
        return 1
    }

    info_log "Propagator $image deployed at $CEDANA_URL"
}

teardown_propagator() {
    if [ -n "$PROPAGATOR_PORT_FORWARD_PID" ]; then
        pkill -P "$PROPAGATOR_PORT_FORWARD_PID" 2>/dev/null || true
        kill "$PROPAGATOR_PORT_FORWARD_PID" 2>/dev/null || true
        PROPAGATOR_PORT_FORWARD_PID=""
        ip addr del "$PROPAGATOR_IP/32" dev lo 2>/dev/null || sudo -n ip addr del "$PROPAGATOR_IP/32" dev lo 2>/dev/null || true
    fi

    if kubectl get namespace "$PROPAGATOR_NAMESPACE" &>/dev/null; then
        kubectl logs -n "$PROPAGATOR_NAMESPACE" deployment/cedana-propagator --tail=-1 \
            >"$PROPAGATOR_LOG_FILE" 2>&1 || true
        delete_namespace "$PROPAGATOR_NAMESPACE" --wait=false
    fi
}
