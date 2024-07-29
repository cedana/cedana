FROM golang:1.22-bookworm as builder
ARG VERSION="dev"

WORKDIR /app
ADD . /app

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive \
    apt-get install --no-install-recommends --assume-yes \
    wget git make curl libnl-3-dev libnet-dev libbsd-dev runc libcap-dev \
    libgpgme-dev btrfs-progs libbtrfs-dev libseccomp-dev libapparmor-dev libprotobuf-dev \
    libprotobuf-c-dev protobuf-c-compiler protobuf-compiler python3-protobuf software-properties-common zip

RUN NOW=$(date +'%Y-%m-%d') \
    && COMMIT=$(git rev-parse HEAD) \
    && VERSION=$VERSION \
    && CGO_ENABLED=1 go build -o cedana -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$NOW"

FROM ubuntu:22.04

# Install essential packages
RUN apt-get update && \
    apt-get install -y software-properties-common git wget zip && \
    apt-get install -y libgpgme-dev libseccomp-dev libbtrfs-dev && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/cedana /usr/local/bin/
COPY --from=builder /app/build-start-daemon.sh /usr/local/bin/

ENV USER="root"

ENTRYPOINT ["/bin/bash"]
