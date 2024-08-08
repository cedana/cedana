FROM golang:1.22-bookworm as builder

WORKDIR /app
ADD . /app

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive \
    apt-get install --no-install-recommends --assume-yes \
    wget git make curl libnl-3-dev libnet-dev libbsd-dev runc libcap-dev \
    libgpgme-dev btrfs-progs libbtrfs-dev libseccomp-dev libapparmor-dev libprotobuf-dev \
    libprotobuf-c-dev protobuf-c-compiler protobuf-compiler python3-protobuf software-properties-common zip

RUN /app/build.sh

RUN curl --proto '=https' --tlsv1.2 -fOL \
    https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.106.1/otelcol-contrib_0.106.1_linux_amd64.tar.gz && \
    tar -xvf otelcol-contrib_0.106.1_linux_amd64.tar.gz

FROM ubuntu:22.04

# Install essential packages
RUN apt-get update && \
    apt-get install -y software-properties-common git wget zip && \
    apt-get install -y libgpgme-dev libseccomp-dev libbtrfs-dev && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/cedana /usr/local/bin/
COPY --from=builder /app/build.sh /usr/local/bin/
COPY --from=builder /app/build-start-daemon.sh /usr/local/bin/
COPY --from=builder /app/stop-daemon.sh /usr/local/bin/
COPY --from=builder /app/otelcol-contrib /usr/local/bin
COPY --from=builder /app/scripts/otelcol-config.yaml /usr/local/bin

ENV USER="root"

ENTRYPOINT ["/bin/bash"]
