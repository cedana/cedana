# syntax=docker/dockerfile:1.6

FROM cedana/cedana-samples:cuda12.4-torch2.4 AS cedana-samples

FROM ubuntu:24.04
LABEL org.opencontainers.image.source https://github.com/cedana/cedana

ARG GO_VERSION=1.23.0

# install packages
RUN <<EOT
set -eux
APT_PACKAGES="build-essential sudo wget git make curl libnl-3-dev libnet-dev lsof psmisc \
    pkg-config libbsd-dev containerd runc libcap-dev libgpgme-dev iptables iproute2 \
    libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler \
    protobuf-compiler python3-protobuf software-properties-common \
    openmpi-bin libopenmpi-dev openmpi-doc \
    zip jq
"
apt-get update
for pkg in $APT_PACKAGES; do
    apt-get install -y $pkg || echo "failed to install $pkg" >&2
done
EOT

# install bats
RUN <<EOT
set -eux
apt-get install -y bats bats-assert bats-support bats-file
EOT

# install otelcol
RUN <<EOT
curl --proto '=https' --tlsv1.2 -fOL https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.114.0/otelcol_0.114.0_linux_amd64.tar.gz
tar -xvf otelcol_0.114.0_linux_amd64.tar.gz
mv otelcol /usr/local/bin/
EOT

# install go
RUN <<EOT
set -eux
wget https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz && rm -rf /usr/local/go
tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz && rm go${GO_VERSION}.linux-amd64.tar.gz
EOT
ENV PATH=${PATH}:/usr/local/go/bin:${GOPATH}/bin:/usr/local/bin

# copy cedana-samples
COPY --from=cedana-samples /app /cedana-samples

VOLUME ["/src"]
WORKDIR /src
ENV PATH=${PATH}:/src
RUN git config --global --add safe.directory `pwd`
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

CMD ["/entrypoint.sh"]
