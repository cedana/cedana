#!/bin/bash
# Used to run a quick smoke test for CI
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -

add-apt-repository \
    "deb [arch=amd64] https://download.docker.com/linux/ubuntu \
   $(lsb_release -cs) \
   stable test"

./apt-install.sh docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

if [ -z "$DOCKERHUB_TOKEN" ]; then
    echo "DOCKERHUB_TOKEN is not set"
    exit 1
fi

sudo docker pull cedana/cedana-benchmarking:latest

echo '{"client":{"leave_running":false, "task":""}}' >client_config.json
uname -r

BRANCH_NAME="${CI_BRANCH:-main}"
sudo docker run \
    -e CEDANA_OTEL_ENABLED=false \
    -v ${PWD}/client_config.json:/root/.cedana/client_config.json \
    --cap-add=SYS_ADMIN \
    --privileged --tmpfs /run \
    --entrypoint /bin/bash \
    cedana/cedana-benchmarking:latest -c "
    git fetch origin &&
    git checkout ${BRANCH_NAME} &&
    git pull origin ${BRANCH_NAME} &&
    ./build-start-daemon.sh &&
    sudo -E python3 test/benchmarks/performance.py --smoke --num_samples 3
"
