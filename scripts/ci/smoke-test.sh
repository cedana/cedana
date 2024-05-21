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

print_env() {
    set +x
    print_header "Environment variables"
    printenv
    print_header "uname -a"
    uname -a || :
    print_header "Mounted file systems"
    cat /proc/self/mountinfo || :
    print_header "Kernel command line"
    cat /proc/cmdline || :
    print_header "Kernel modules"
    lsmod || :
    print_header "Distribution information"
    [ -e /etc/lsb-release ] && cat /etc/lsb-release
    [ -e /etc/redhat-release ] && cat /etc/redhat-release
    [ -e /etc/alpine-release ] && cat /etc/alpine-release
    print_header "ulimit -a"
    ulimit -a
    print_header "Available memory"
    if [ -e /etc/alpine-release ]; then
        # Alpine's busybox based free does not understand -h
        free
    else
        free -h
    fi
    print_header "Available CPUs"
    lscpu || :
    set -x
}

print_env

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
