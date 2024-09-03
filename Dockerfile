FROM golang:1.22-bullseye as builder

WORKDIR /app
ADD . /app

RUN <<EOT
set -eux
DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y wget git make curl libnl-3-dev libnet-dev libbsd-dev runc libcap-dev
apt-get install -y libgpgme-dev btrfs-progs libbtrfs-dev libseccomp-dev libapparmor-dev libprotobuf-dev
apt-get install -y libprotobuf-c-dev protobuf-c-compiler protobuf-compiler python3-protobuf
apt-get install -y software-properties-common zip

curl --proto '=https' --tlsv1.2 -fOL https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.106.1/otelcol-contrib_0.106.1_linux_amd64.tar.gz
tar -xvf otelcol-contrib_0.106.1_linux_amd64.tar.gz

EOT

RUN /app/build.sh

FROM ubuntu:22.04

RUN <<EOT
set -eux
apt-get update
apt-get install -y software-properties-common git wget zip
apt-get install -y libgpgme-dev libseccomp-dev libbtrfs-dev btrfs-progs
rm -rf /var/lib/apt/lists/*
EOT

COPY --from=builder /app/cedana /usr/local/bin/
COPY --from=builder /app/build.sh /usr/local/bin/
COPY --from=builder /app/build-start-daemon.sh /usr/local/bin/
COPY --from=builder /app/stop-daemon.sh /usr/local/bin/
COPY --from=builder /app/otelcol-contrib /usr/local/bin/otelcol-contrib
COPY --from=builder /app/scripts/otelcol-config.yaml /usr/local/bin/otelcol-config.yaml

ENV USER="root"

ENTRYPOINT ["/bin/bash"]
