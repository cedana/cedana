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

if [ -z "$CHECKPOINTSVC_URL" ]
then
    echo "CHECKPOINTSVC_URL is not set"
    exit 1
fi

if [ -z "$CHECKPOINTSVC_TOKEN" ]
then 
    echo "CHECKPOINT_SVC_TOKEN is not set"
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


function setup_benchmarking() {
    cd test/benchmarks
    pip install -r requirements

    protoc --python_out=. profile.proto
    cd ../..
}
function start_benchmarking() {
    echo "Running benchmarking script from $(pwd)"
    CEDANA_REMOTE=true
    CEDANA_OTEL_ENABLED=true
    ./test/benchmarks/entrypoint.sh
}

main() {
    print_env || { echo "Failed to print env"; exit 1; }
    setup_ci || { echo "Failed to setup CI"; exit 1; }
    setup_benchmarking || { echo "Failed to setup benchmarking"; exit 1; }
    start_benchmarking || { echo "Failed to start benchmarking"; exit 1; }
}

main
