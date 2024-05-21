#!/bin/bash
# Used to run a quick smoke test for CI
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -

add-apt-repository \
    "deb [arch=amd64] https://download.docker.com/linux/ubuntu \
   $(lsb_release -cs) \
   stable test"

APT_PACKAGES="wget git make libnl-3-dev libnet-dev \
    libbsd-dev python-ipaddress libcap-dev \
    libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler \
    protobuf-compiler python3-protobuf"

install_apt_packages() {
    apt-get update
    apt-get install -y $APT_PACKAGES
}

if [ -z "$DOCKERHUB_TOKEN" ]; then
    echo "DOCKERHUB_TOKEN is not set"
    exit 1
fi

print_header() {
    echo "############### $1 ###############"
}

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

setup_ci() {
    install_apt_packages
    wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz && rm -rf /usr/local/go
    tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz && rm go1.22.0.linux-amd64.tar.gz

    export PATH=$PATH:/usr/local/go/bin
    echo "export PATH=$PATH:/usr/local/go/bin" >>/root/.bashrc

    # Install CRIU
    git clone https://github.com/checkpoint-restore/criu.git && cd /criu
    git checkout master && git pull
    make
    cp criu/criu /usr/local/bin/criu
    cd /

    # Install Cedana
    git clone https://github.com/cedana/cedana && mkdir ~/.cedana
    git fetch && git checkout ${BRANCH_NAME} && git pull origin ${BRANCH_NAME}

    # Install smoke & bench deps
    pip3 install -r test/benchmarks/requirements.txt
}

start_cedana() {
    ./build-start-daemon.sh
}

start_smoke() {
    sudo -E python3 test/benchmarks/performance.py --smoke --num_samples 3
}

print_env

echo '{"client":{"leave_running":false, "task":""}}' >~/.cedana/client_config.json
BRANCH_NAME="${CI_BRANCH:-main}"

setup_ci
start_cedana
start_smoke
