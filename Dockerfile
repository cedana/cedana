FROM golang:1.22-bookworm as builder

WORKDIR /app
ADD . /app

RUN apt-get update
RUN apt-get install -y wget git make curl libnl-3-dev libnet-dev libbsd-dev runc libcap-dev \
    libgpgme-dev btrfs-progs libbtrfs-dev libseccomp-dev libapparmor-dev libprotobuf-dev \
    libprotobuf-c-dev protobuf-c-compiler protobuf-compiler python3-protobuf software-properties-common zip

RUN CGO_ENABLED=1 go build -o cedana

FROM ubuntu:22.04

COPY --from=builder /app/cedana /usr/local/bin/
COPY --from=builder /app/build-start-daemon.sh /usr/local/bin/

ENV USER="root"

ENTRYPOINT ["/bin/bash"]
