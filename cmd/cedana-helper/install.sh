#!/bin/bash

chroot /host <<"EOT"

## These steps are unecessary if we're using a Cedana AMI or image (that has these dependencies preinstalled).

# Check whether git is already installed
if ! command -v git &> /dev/null; then
    yum install -y git
fi

# Download and install Go
wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
rm -rf /usr/local/go
tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz
rm go1.22.0.linux-amd64.tar.gz

# Add Go binary directory to PATH
export PATH=$PATH:/usr/local/go/bin

# Add export statement to ~/.bashrc
echo "export PATH=$PATH:/usr/local/go/bin" >> /root/.bashrc

# Install required packages
yum install -y libnet-devel protobuf protobuf-c protobuf-c-devel protobuf-compiler protobuf-devel protobuf-python libnl3-devel libcap-devel
yum group install -y "Development Tools"

# Clone CRIU repository and build
git clone https://github.com/checkpoint-restore/criu.git
cd /criu
git checkout master
git pull
make
cp criu/criu /usr/local/bin
cd /

# Clone Cedana repository and build
git clone https://github.com/cedana/cedana.git

LINE="export CEDANA_IS_K8S=1"

# Add the line to the .bashrc file
echo "$LINE" >> ~/.bashrc

source ~/.bashrc

cd cedana
git checkout hotfix/arch
./build-start-daemon.sh&
EOT
