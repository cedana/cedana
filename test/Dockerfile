# syntax=docker/dockerfile:1.6

FROM cedana/cedana-samples:cpu AS cedana-samples

FROM ubuntu:24.04
LABEL org.opencontainers.image.source https://github.com/cedana/cedana

ARG GO_VERSION=1.25.1
ARG KUBECTL_VERSION=1.33.0
ARG K9S_VERSION=latest

# install packages
RUN <<EOT
set -eux
APT_PACKAGES="build-essential sudo wget git make curl libnftables1 libnl-3-dev libnet-dev lsof psmisc \
    pkg-config libbsd-dev libcap-dev libgpgme-dev iptables iproute2 \
    libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler \
    protobuf-compiler python3-protobuf software-properties-common \
    containerd zip unzip jq
"
apt-get update
for pkg in $APT_PACKAGES; do
    apt-get install -y --no-install-recommends $pkg || echo "failed to install $pkg" >&2
done
EOT

# install bats
RUN <<EOT
set -eux
apt-get install -y bats bats-assert bats-support bats-file
EOT

# install rush
RUN <<EOT
set -eux
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)
        RUSH_ARCH="amd64"
        ;;
    aarch64|arm64)
        RUSH_ARCH="arm64"
        ;;
    *)
        echo "Unsupported architecture for rush: $ARCH"
        exit 1
        ;;
esac
wget https://github.com/shenwei356/rush/releases/latest/download/rush_linux_"${RUSH_ARCH}".tar.gz
tar -C /usr/local/bin -xzf rush_linux_"${RUSH_ARCH}".tar.gz rush
rm rush_linux_"${RUSH_ARCH}".tar.gz
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

# Install docker-cli
RUN <<EOT
sudo mkdir -m 0755 -p /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
apt update && apt install -y docker-ce-cli
EOT

# Configure containerd
RUN <<'EOT'
mkdir -p /etc/containerd
cat > /etc/containerd/config.toml <<'CONFIG'
version = 2

[plugins]
  [plugins."io.containerd.grpc.v1.cri"]
    disable_tcp_service = true
    stream_server_address = "127.0.0.1"
    stream_server_port = "0"
    enable_selinux = false
    sandbox_image = "registry.k8s.io/pause:3.8"

    [plugins."io.containerd.grpc.v1.cri".containerd]
      snapshotter = "native"
      default_runtime_name = "runc"

      [plugins."io.containerd.grpc.v1.cri".containerd.runtimes]
        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
          runtime_type = "io.containerd.runc.v2"
CONFIG
EOT

# copy cedana-samples
COPY --from=cedana-samples /app /cedana-samples

VOLUME ["/src"]
WORKDIR /src
RUN git config --global --add safe.directory `pwd`

CMD []
