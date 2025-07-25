# syntax=docker/dockerfile:1.6

FROM cedana/cedana-samples:cuda12.4-torch2.4 AS cedana-samples

FROM ubuntu:24.04
LABEL org.opencontainers.image.source https://github.com/cedana/cedana

ARG GO_VERSION=1.24.3
ARG KUBECTL_VERSION=1.33.0
ARG K9S_VERSION=latest

# install packages
RUN <<EOT
set -eux
APT_PACKAGES="build-essential sudo wget git make curl libnl-3-dev libnet-dev lsof psmisc \
    pkg-config libbsd-dev containerd runc libcap-dev libgpgme-dev iptables iproute2 \
    libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler \
    protobuf-compiler python3-protobuf software-properties-common \
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

# Setup go
RUN <<EOT
set -eux
if [ $(uname -m) = "aarch64" ]; then
    ARCH=arm64
elif [ $(uname -m) = "x86_64" ]; then
    ARCH=amd64
else
    echo "Unsupported architecture: $(uname -m)"
    exit 1
fi
wget https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz
rm -rf /usr/local/go
tar -C /usr/local -xzf go${GO_VERSION}.linux-${ARCH}.tar.gz
ln -s /usr/local/go/bin/go /usr/local/bin/go
rm go${GO_VERSION}.linux-${ARCH}.tar.gz
EOT

# Install Docker (CLI only)
RUN <<EOT
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
echo \
    "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] \
    https://download.docker.com/linux/ubuntu \
    $(. /etc/os-release && echo "$UBUNTU_CODENAME") stable" | \
tee /etc/apt/sources.list.d/docker.list > /dev/null
apt-get update
apt-get install -y docker-ce docker-ce-cli
EOT

# install kubectl
RUN <<EOT
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)
        KC_ARCH="amd64"
        ;;
    aarch64|arm64)
        KC_ARCH="arm64"
        ;;
    *)
        error_log "Unsupported architecture for kubectl: $ARCH"
        return 1
        ;;
esac
curl -LO https://dl.k8s.io/release/v${KUBECTL_VERSION}/bin/linux/${KC_ARCH}/kubectl
install -m 0755 kubectl /usr/local/bin/kubectl
rm -f kubectl
EOT

# install helm
RUN <<EOT
curl -fsSL -o /tmp/get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
chmod 700 /tmp/get_helm.sh
/tmp/get_helm.sh
rm -f /tmp/get_helm.sh
EOT

# install k9s
RUN <<EOT
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)
        K9S_ARCH="amd64"
        ;;
    aarch64|arm64)
        K9S_ARCH="arm64"
        ;;
    *)
        error_log "Unsupported architecture for k9s: $ARCH"
        return 1
        ;;
esac
wget https://github.com/derailed/k9s/releases/${K9S_VERSION}/download/k9s_linux_"${K9S_ARCH}".deb -O /tmp/k9s.deb
apt install -y /tmp/k9s.deb
rm -f /tmp/k9s.deb
EOT

# copy cedana-samples
COPY --from=cedana-samples /app /cedana-samples

VOLUME ["/src"]
WORKDIR /src
ENV PATH=${PATH}:/src
RUN git config --global --add safe.directory `pwd`
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

CMD ["/entrypoint.sh"]
