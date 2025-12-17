# syntax=docker/dockerfile:1.6
FROM golang:1.25-rc-bullseye as builder

ARG PREBUILT_BINARIES=0
ARG ALL_PLUGINS=0
ARG VERSION

WORKDIR /app

# Copy dependency files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy Makefile and scripts needed for build
COPY Makefile ./
COPY scripts/ ./scripts/

# Copy source code
COPY . .

RUN <<EOT
make reset
if [ "$PREBUILT_BINARIES" -ne "1" ]; then
  if [ "$ALL_PLUGINS" -eq "1" ]; then
    make cedana plugins -j $(nproc) VERSION=${VERSION}
  else
    make cedana ${PWD}/libcedana-k8s.so -j $(nproc) VERSION=${VERSION}
  fi
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
RUN chmod +x /usr/local/bin/cedana

ENV USER="root"

ENTRYPOINT ["/bin/bash"]
