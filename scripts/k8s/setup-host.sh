#!/bin/bash

chroot /host <<"EOT"

# of course, there's differences in the names of some of these packages
YUM_PACKAGES="wget git gcc make libnet-devel protobuf \
    protobuf-c protobuf-c-devel protobuf-compiler \
    protobuf-devel protobuf-python libnl3-devel \
    libcap-devel"

APT_PACKAGES="wget git make libnl-3-dev libnet-dev \
    libbsd-dev python-ipaddress libcap-dev \
    libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler \
    protobuf-compiler python3-protobuf"

install_apt_packages() {
    apt-get update
    apt-get install -y $APT_PACKAGES
}

install_yum_packages() {
    yum install -y $YUM_PACKAGES
    yum group install -y "Development Tools"
}

if [ -f /etc/debian_version ]; then
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
git checkout master && git pull
make
cp criu/criu /usr/local/bin/criu
cd /

# Install Cedana
git clone https://github.com/cedana/cedana.git
echo "export IS_K8S=1" >> ~/.bashrc
source ~/.bashrc

cd cedana && go build -v
cp cedana /usr/local/bin/cedana

cedana daemon start &

EOT
