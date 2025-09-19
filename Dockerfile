# syntax=docker/dockerfile:1.6
FROM golang:1.24-bullseye as builder

WORKDIR /app

RUN <<EOT
set -eux
DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y wget git make curl libnl-3-dev libnet-dev libbsd-dev runc libcap-dev
apt-get install -y libgpgme-dev btrfs-progs libbtrfs-dev libseccomp-dev libapparmor-dev libprotobuf-dev
apt-get install -y libprotobuf-c-dev protobuf-c-compiler protobuf-compiler python3-protobuf
apt-get install -y software-properties-common zip
apt-get install -y protobuf-compiler build-essential
EOT

RUN <<EOT
curl https://sh.rustup.rs -sSf | sh -s -- -y
. $HOME/.cargo/env
# Buildah & Netavark (Netavark required for latest versions of buildah)
git clone https://github.com/containers/buildah.git /app/buildah
git clone https://github.com/containers/netavark.git /app/netavark

cd /app/buildah
git checkout v1.37.3
cd cmd/buildah
go build .

cd /app/netavark
git checkout v1.12.2
make V=1
EOT

ADD ./go.mod /app
ADD ./go.sum /app
RUN go mod download && rm -rf go.mod go.sum
ADD . /app
RUN make cedana plugins -j $(nproc)

FROM ubuntu:22.04

RUN <<EOT
set -eux
DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y software-properties-common git wget zip make sudo
apt-get install -y libgpgme-dev libseccomp-dev libbtrfs-dev btrfs-progs
rm -rf /var/lib/apt/lists/*
EOT

RUN mkdir -p /usr/local/bin /scripts

ADD ./Makefile /
ADD ./scripts/ /scripts

COPY --from=builder /app/libcedana*.so /usr/local/lib/
COPY --from=builder /app/cedana /usr/local/bin/
COPY --from=builder /app/buildah/cmd/buildah/buildah /usr/local/bin
COPY --from=builder /app/netavark/bin/netavark /usr/local/bin
COPY --from=builder /app/netavark/bin/netavark-dhcp-proxy-client /usr/local/bin

ENV USER="root"

ENTRYPOINT ["/bin/bash"]
