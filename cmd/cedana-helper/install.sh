#!/bin/bash

# Check whether git is already installed
if ! command -v git &> /dev/null; then
    yum install -y git
fi

# Download and install Go
wget https://go.dev/dl/go1.22.0.linux-arm64.tar.gz
rm -rf /usr/local/go
tar -C /usr/local -xzf go1.22.0.linux-arm64.tar.gz

# Add Go binary directory to PATH
export PATH=$PATH:/usr/local/go/bin

# Add export statement to ~/.bashrc
echo "export PATH=$PATH:/usr/local/go/bin" >> /root/.bashrc

# Install required packages
yum install -y libnet-devel protobuf protobuf-c protobuf-c-devel protobuf-compiler protobuf-devel protobuf-python libnl3-devel libcap-devel
yum group install -y "Development Tools"

# Clone CRIU repository and build
git clone https://github.com/checkpoint-restore/criu.git
cd criu
make
cp criu/criu /usr/local/bin
cd /

# Clone Cedana repository and build
git clone https://github.com/cedana/cedana.git
cd cedana
go build -o /cedana
cd /
