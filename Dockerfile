FROM golang:1.22-bullseye as builder

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

RUN curl --proto '=https' --tlsv1.2 -fOL https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.106.1/otelcol-contrib_0.106.1_linux_amd64.tar.gz && \
    tar -xvf otelcol-contrib_0.106.1_linux_amd64.tar.gz


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


RUN curl --proto '=https' --tlsv1.2 -fOL https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.106.1/otelcol-contrib_0.106.1_linux_amd64.tar.gz && \
    tar -xvf otelcol-contrib_0.106.1_linux_amd64.tar.gz

ADD ./go.mod /app
ADD ./go.sum /app
RUN go mod download && rm -rf go.mod go.sum
ADD . /app
RUN /app/build.sh

FROM ubuntu:22.04

RUN <<EOT
set -eux
DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y software-properties-common git wget zip
apt-get install -y libgpgme-dev libseccomp-dev libbtrfs-dev btrfs-progs
rm -rf /var/lib/apt/lists/*
EOT

COPY ./build.sh /usr/local/bin/
COPY ./build-start-daemon.sh /usr/local/bin/
COPY ./setup-host.sh /usr/local/bin/
COPY ./stop-daemon.sh /usr/local/bin/
COPY ./scripts/otelcol-config.yaml /usr/local/bin/otelcol-config.yaml

COPY --from=builder /app/otelcol-contrib /usr/local/bin/otelcol-contrib
COPY --from=builder /app/cedana /usr/local/bin/
COPY --from=builder /app/buildah/cmd/buildah/buildah /usr/local/bin
COPY --from=builder /app/netavark/bin/netavark /usr/local/bin
COPY --from=builder /app/netavark/bin/netavark-dhcp-proxy-client /usr/local/bin

ENV USER="root"

ENTRYPOINT ["/bin/bash"]
