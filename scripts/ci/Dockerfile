# syntax=docker/dockerfile:1.6
# This docker file can be used to set up a container for CPU benchmarks only
# This image has docker inside, that runs as a proper daemon.

FROM ubuntu:24.04
LABEL org.opencontainers.image.source https://github.com/cedana/cedana

# install packages
RUN <<EOT
set -eux
APT_PACKAGES="build-essential sudo wget git make curl libnl-3-dev libnet-dev \
    libbsd-dev runc libcap-dev libgpgme-dev iptables iproute2 \
    btrfs-progs libbtrfs-dev libseccomp-dev libapparmor-dev \
    libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler \
    protobuf-compiler python3-protobuf software-properties-common \
    zip
"
apt-get update
for pkg in $APT_PACKAGES; do
    apt-get install -y $pkg || echo "failed to install $pkg"
done
EOT

# install code server
RUN curl -fsSL https://code-server.dev/install.sh | sh

# install bats core
RUN <<EOT
set -eux
git clone https://github.com/bats-core/bats-core.git
cd bats-core
./install.sh /usr/local
cd .. && rm -rf bats-core
EOT

# install docker
RUN <<EOT
set -eux
sudo apt-get update
sudo apt-get install ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
    $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
EOT

# install sysbox
RUN <<EOT
set -eux
wget https://downloads.nestybox.com/sysbox/releases/v0.6.4/sysbox-ce_0.6.4-0.linux_amd64.deb
apt-get install -y jq
apt-get install -y ./sysbox-ce_0.6.4-0.linux_amd64.deb
rm -f sysbox-ce_0.6.4-0.linux_amd64.deb
EOT

# install otelcol
RUN <<EOT
set -eux
wget https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.94.0/otelcol-contrib_0.94.0_linux_amd64.deb
dpkg-deb -x otelcol-contrib_0.94.0_linux_amd64.deb extracted/ && cp extracted/usr/bin/otelcol-contrib /usr/bin/otelcol-contrib
rm -rf extracted/ otelcol-contrib_0.94.0_linux_amd64.deb
EOT

# install buildah
RUN apt-get -y install buildah

# install crictl
RUN <<EOT
set -eux
VERSION="v1.30.0"
curl -L https://github.com/kubernetes-sigs/cri-tools/releases/download/$VERSION/crictl-${VERSION}-linux-amd64.tar.gz --output crictl-${VERSION}-linux-amd64.tar.gz
tar zxvf crictl-$VERSION-linux-amd64.tar.gz -C /usr/local/bin
rm -f crictl-$VERSION-linux-amd64.tar.gz
EOT

# install go
# RUN <<EOT
# set -eux
# wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz && rm -rf /usr/local/go
# tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz && rm go1.22.0.linux-amd64.tar.gz
# EOT

# install CRIU
RUN <<EOT
set -eux
wget https://download.opensuse.org/repositories/devel:/tools:/criu/xUbuntu_24.04/amd64/criu_4.0-3_amd64.deb
dpkg -i criu_4.0-3_amd64.deb
rm criu_4.0-3_amd64.deb
EOT

# setup cedana
RUN <<EOT
set -eux
mkdir -p $HOME/.cedana
echo '{"client":{"leave_running":false, "task":""}, "connection":{"cedana_url": "https://ci.cedana.ai"}}' > $HOME/.cedana/client_config.json
EOT

# update paths
ENV PATH=${PATH}:/usr/local/go/bin:${GOPATH}/bin:/usr/local/bin
ENV CEDANA_URL=https://ci.cedana.ai

# mount your source code here
VOLUME ["/src"]
WORKDIR /src
CMD ["/bin/bash"]
