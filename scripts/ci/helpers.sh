#!/bin/bash


## NOTE: All scripts are being run by the makefile, which runs in the scripts/ci directory.
## As a result, where these functions are called rely on managing directory state using pushd/popd,
## which also means all these functions assume they're being run in the root directory.
## Look at regression-test main for an example.
##

APT_PACKAGES="wget git make curl libnl-3-dev libnet-dev \
    libbsd-dev runc libcap-dev libgpgme-dev \
    btrfs-progs libbtrfs-dev libseccomp-dev libapparmor-dev \
    libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler \
    protobuf-compiler python3-protobuf software-properties-common \
    zip
"

chmod 1777 /tmp

install_apt_packages() {
    apt-get update
    for pkg in $APT_PACKAGES; do
        apt-get install -y $pkg || echo "failed to install $pkg"
    done
}

install_code_server() {
    curl -fsSL https://code-server.dev/install.sh | sh
}

install_bats_core() {
    if ! command -v bats &> /dev/null; then
        git clone https://github.com/bats-core/bats-core.git
        pushd bats-core
        ./install.sh /usr/local
        popd && rm -rf bats-core
    else
        installed_version=`bats -v`
        echo "BATS installed: $installed_version"
    fi
}

install_docker() {
    sudo apt-get update
    sudo apt-get install ca-certificates curl
    sudo install -m 0755 -d /etc/apt/keyrings
    sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
    sudo chmod a+r /etc/apt/keyrings/docker.asc

# Add the repository to Apt sources:
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
        $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
    sudo apt-get update
    sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
}

install_sysbox() {
    wget https://downloads.nestybox.com/sysbox/releases/v0.6.4/sysbox-ce_0.6.4-0.linux_amd64.deb
    apt-get install -y jq
    apt-get install -y ./sysbox-ce_0.6.4-0.linux_amd64.deb
    rm -f sysbox-ce_0.6.4-0.linux_amd64.deb
}

install_buildah() {
    sudo apt-get update
    sudo apt-get -y install buildah
}

install_crictl() {
    VERSION="v1.30.0"
    curl -L https://github.com/kubernetes-sigs/cri-tools/releases/download/$VERSION/crictl-${VERSION}-linux-amd64.tar.gz --output crictl-${VERSION}-linux-amd64.tar.gz
    sudo tar zxvf crictl-$VERSION-linux-amd64.tar.gz -C /usr/local/bin
    rm -f crictl-$VERSION-linux-amd64.tar.gz

}

install_otelcol_contrib() {
    if [ ! -f otelcol-contrib_0.94.0_linux_amd64.deb ]; then
        wget https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.94.0/otelcol-contrib_0.94.0_linux_amd64.deb
    fi
    dpkg-deb -x otelcol-contrib_0.94.0_linux_amd64.deb extracted/ && cp extracted/usr/bin/otelcol-contrib /usr/bin/otelcol-contrib
}

# Function to compare Go versions
compare_versions() {
    printf '%s\n%s\n' "$1" "$2" | sort -V | head -n 1
}

install_golang() {
    if ! command -v go &> /dev/null; then
        echo "Golang is not installed. Installing Go 1.22.6..."
        wget https://go.dev/dl/go1.22.6.linux-amd64.tar.gz
        sudo tar -C /usr/local -xzf go1.22.6.linux-amd64.tar.gz
        
        # Set GOPATH and update PATH
        echo "export GOPATH=$HOME/go" >> /etc/environment
        echo "export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin:$GOPATH/bin" >> /etc/environment
        echo "Go 1.22.6 installed successfully."
    else
        installed_version=`go version`
        echo "Installed Go version: $installed_version"
    fi
}

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

setup_ci_build() {
    # only CI steps needed for building
    [ -n "$SKIP_CI_SETUP" ] && return
    print_header "Installing APT Packages"
    install_apt_packages

    print_header "Installing Golang"
    install_golang
}

setup_ci() {
    setup_ci_build
    print_header "Installing the VS Code Server"
    install_code_server

    print_header "Installing the BATS Core"
    install_bats_core

    print_header "Installing Docker"
    install_docker

    print_header "Installing Sysbox"
    install_sysbox

    print_header "Installing OpenTelemetry Collector"
    install_otelcol_contrib

    print_header "Installing buildah"
    install_buildah

    print_header "Installing Crictl"
    install_crictl

    mkdir -p $HOME/.cedana
    echo '{"client":{"leave_running":false, "task":""}, "connection":{"cedana_url": "https://ci.cedana.ai"}}' > $HOME/.cedana/client_config.json

    print_header "Installing recvtty"
    go install github.com/opencontainers/runc/contrib/cmd/recvtty@latest

    echo "export CEDANA_URL=https://ci.cedana.ai" >> /etc/environment

    print_header "Installing CRIU"
    sudo add-apt-repository -y ppa:criu/ppa
    sudo apt-get update && sudo apt-get install -y criu

    print_header "Installing python dependencies for benchmark and smoke"
    sudo pip3 install -r test/benchmarks/requirements
}

source_env() {
    source /etc/environment
}

start_otelcol() {
    otelcol-contrib --config test/benchmarks/otelcol-config.yaml &
}

start_cedana() {
    ./build-start-daemon.sh --no-build
}

stop_cedana() {
    ./reset.sh
}
