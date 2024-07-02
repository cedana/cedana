#!/bin/bash 

source ./helpers.sh


if [ -z "$DOCKERHUB_TOKEN" ]
then
    echo "DOCKERHUB_TOKEN is not set"
    exit 1
fi 

if [ -z "$GOOGLE_APPLICATION_CREDENTIALS" ]
then
    echo "GOOGLE_APPLICATION_CREDENTIALS is not set"
    exit 1
fi 

if [ -z "$CEDANA_URL" ]
then
    echo "CHECKPOINTSVC_URL is not set"
    exit 1
fi

if [ -z "$BENCHMARK_ACCOUNT" ]
then 
    echo "BENCHMARK_ACCOUNT is not set"
    exit 1
fi

if [ -z "$BENCHMARK_ACCOUNT_PW" ]
then
    echo "BENCHMARK_ACCOUNT_PW is not set"
    exit 1
fi

if [ -z "$SIGNOZ_ACCESS_TOKEN" ]
then
    echo "SIGNOZ_ACCESS_TOKEN is not set"
    exit 1
fi

CONTAINER_CREDENTIAL_PATH=/tmp/creds.json 

echo '{"client":{"leave_running":false, "task":""}, "connection": {"cedana_auth_token": "'$CHECKPOINTSVC_TOKEN'", "cedana_url": "'$CHECKPOINTSVC_URL'", "cedana_user": "benchmark"}}' > client_config.json
cat client_config.json
mkdir -p ~/.cedana
cp client_config.json ~/.cedana/client_config.json

function get_auth_token() {
    local LOGIN_URL
    local AUTH_TOKEN

    LOGIN_URL=$(curl -s -X GET -H "Accept: application/json" 'https://auth.cedana.com/self-service/login/api' | jq -r '.ui.action')
    if [ -z "$LOGIN_URL" ]; then
        echo "Failed to retrieve LOGIN_URL" >&2
        return 1
    fi

    AUTH_TOKEN=$(curl -s -X POST -H "Accept: application/json" -H "Content-Type: application/json" \
        -d '{"identifier": "'"$BENCHMARK_ACCOUNT"'", "password": "'"$BENCHMARK_ACCOUNT_PW"'", "method": "password"}' \
        "$LOGIN_URL" | jq -r '.session_token')
    if [ -z "$AUTH_TOKEN" ]; then
        echo "Failed to retrieve AUTH_TOKEN" >&2
        return 1
    fi

    echo "$AUTH_TOKEN"
}

function setup_benchmarking() {
    cd test/benchmarks
    pip install -r requirements

    protoc --python_out=. profile.proto
    cd ../..
}

echo '{"client":{"leave_running":false, "task":""}, "connection": {"cedana_auth_token": "'$(get_auth_token)'", "cedana_url": "'$CHECKPOINTSVC_URL'", "cedana_user": "benchmark"}}' > client_config.json
cat client_config.json
mkdir -p ~/.cedana
cp client_config.json ~/.cedana/client_config.json


function start_benchmarking() {
    echo "Running benchmarking script from $(pwd)"
    export CEDANA_REMOTE=true
    export CEDANA_OTEL_ENABLED=true
    export CEDANA_AUTH_TOKEN=$(get_auth_token)
    ./test/benchmarks/entrypoint.sh
}

main() {
    print_env || { echo "Failed to print env"; exit 1; }
    setup_ci || { echo "Failed to setup CI"; exit 1; }
    setup_benchmarking || { echo "Failed to setup benchmarking"; exit 1; }
    start_benchmarking || { echo "Failed to start benchmarking"; exit 1; }
}

main
