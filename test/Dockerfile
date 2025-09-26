# syntax=docker/dockerfile:1.6

FROM cedana/cedana-samples:cpu AS cedana-samples

FROM cruizba/ubuntu-dind:noble-latest
LABEL org.opencontainers.image.source https://github.com/cedana/cedana

ARG GO_VERSION=1.24.3
ARG KUBECTL_VERSION=1.33.0
ARG K9S_VERSION=latest

# install packages
RUN <<EOT
set -eux
APT_PACKAGES="build-essential sudo wget git make curl libnl-3-dev libnet-dev lsof psmisc \
    pkg-config libbsd-dev libcap-dev libgpgme-dev iptables iproute2 \
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

# install go
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

# configure containerd
RUN rm -rf /run/containerd
RUN ln -s /run/docker/containerd /run/containerd

# copy cedana-samples
COPY --from=cedana-samples /app /cedana-samples

VOLUME ["/src"]
WORKDIR /src
ENV PATH=${PATH}:/src
RUN git config --global --add safe.directory `pwd`

# because github actions forces it on us
RUN sed -i '2iumount /var/run/docker.sock || true' /usr/local/bin/entrypoint.sh

ENTRYPOINT ["entrypoint.sh"]
CMD []
