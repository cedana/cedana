#!/bin/bash

chroot /host <<"EOT"

# of course, there's differences in the names of some of these packages
YUM_PACKAGES="wget git gcc make libnet-devel protobuf \
    protobuf-c protobuf-c-devel protobuf-c-compiler \
    protobuf-compiler protobuf-devel python3-protobuf \
    libnl3-devel libcap-devel"

APT_PACKAGES="wget git make libnl-3-dev libnet-dev \
    libbsd-dev libcap-dev pkg-config \
    libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler pkg-config \
    protobuf-compiler python3-protobuf build-essential"

install_apt_packages() {
    apt-get update
    apt-get install -y $APT_PACKAGES
}

install_yum_packages() {
    for pkg in $YUM_PACKAGES; do
        yum install -y $pkg || echo "Failed to install $pkg"
    done
    yum group install -y "Development Tools"
}

if [ -f /etc/os-release ]; then
    . /etc/os-release
    case "$ID" in
        debian | ubuntu)
            install_apt_packages
            ;;
        rhel | centos | fedora)
            install_yum_packages
            ;;
        amzn)
            install_yum_packages
            ;;
        *)
            echo "Unknown distribution"
            exit 1
            ;;
    esac
elif [ -f /etc/debian_version ]; then
    install_apt_packages
elif [ -f /etc/redhat-release ]; then
    install_yum_packages
else
    echo "Unknown distribution"
    exit 1
fi


wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz && rm -rf /usr/local/go
tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz && rm go1.22.0.linux-amd64.tar.gz

export PATH=$PATH:/usr/local/go/bin
echo "export PATH=$PATH:/usr/local/go/bin" >> /root/.bashrc

# Install CRIU
git clone https://github.com/checkpoint-restore/criu.git && cd /criu
git pull
make
cp criu/criu /usr/local/bin/criu
cd /

# Install Cedana
git clone https://github.com/cedana/cedana.git
echo "export IS_K8S=1" >> ~/.bashrc
source ~/.bashrc

cd cedana

./build-start-daemon.sh --systemctl

EOT
