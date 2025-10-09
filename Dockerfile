# syntax=docker/dockerfile:1.6
FROM golang:1.25.2-bullseye as builder

ARG ALL_PLUGINS=0

WORKDIR /app

RUN <<EOT
set -eux
DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y software-properties-common build-essential
EOT

ADD . /app
RUN <<EOT
if [ "$ALL_PLUGINS" -eq "1" ]; then
  make cedana plugins -j $(nproc)
else
  make cedana ${PWD}/libcedana-k8s.so -j $(nproc)
fi
EOT

FROM ubuntu:22.04

RUN <<EOT
set -eux
DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y software-properties-common make sudo
rm -rf /var/lib/apt/lists/*
EOT

RUN mkdir -p /usr/local/bin /scripts

ADD ./Makefile /
ADD ./scripts/ /scripts

COPY --from=builder /app/libcedana*.so /usr/local/lib/
COPY --from=builder /app/cedana /usr/local/bin/

ENV USER="root"

ENTRYPOINT ["/bin/bash"]
