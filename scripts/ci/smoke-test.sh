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

BRANCH_NAME="${CI_BRANCH:-main}" # Default to 'main' if CI_BRANCH is not set
sudo docker run \
    --privileged --tmpfs /run cedana/cedana-benchmarking:latest /bin/bash -c "
    git fetch origin &&
    git checkout ${BRANCH_NAME} &&
    git pull origin ${BRANCH_NAME} &&
    ./build-start-daemon.sh &&
    sudo -E python3 test/benchmarks/performance.py --local &&
"
