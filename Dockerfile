# syntax=docker/dockerfile:1.6

# With PREBUILT_BINARIES=1, the binaries are taken from the build context as is, and the
# builder stage is skipped altogether, as nothing depends on it
ARG PREBUILT_BINARIES=0

FROM rockylinux:8 AS builder

ARG PREBUILT_BINARIES
ARG ALL_PLUGINS=0
ARG VERSION
ARG GO_VERSION=1.25.0

# Install deps
RUN <<EOT
dnf install -y git make gcc findutils
EOT

# Install Golang
RUN <<EOT
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then
  GOARCH="amd64"
elif [ "$ARCH" = "aarch64" ]; then
  GOARCH="arm64"
fi
curl -LO https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz
tar -C /usr/local -xzf go${GO_VERSION}.linux-${GOARCH}.tar.gz
EOT

ENV PATH="/usr/local/go/bin:${PATH}"

ADD . /app
WORKDIR /app
RUN <<EOT
if [ "${PREBUILT_BINARIES}" -ne "1" ]; then
  if [ "${ALL_PLUGINS}" -eq "1" ]; then
    make cedana plugins -j $(nproc) VERSION=${VERSION}
  else
    make cedana ${PWD}/libcedana-k8s.so -j $(nproc) VERSION=${VERSION}
  fi
fi
EOT

FROM builder AS binaries-0

FROM scratch AS binaries-1
COPY libcedana*.so cedana /app/

FROM binaries-${PREBUILT_BINARIES} AS binaries

FROM ubuntu:22.04

RUN <<EOT
set -eux
DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends software-properties-common make sudo
rm -rf /var/lib/apt/lists/*
EOT

COPY --from=binaries /app/libcedana*.so /usr/local/lib/
COPY --from=binaries /app/cedana /usr/local/bin/
RUN chmod +x /usr/local/bin/cedana

ENV USER="root"

ENTRYPOINT ["/bin/bash"]
