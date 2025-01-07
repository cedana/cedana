# syntax=docker/dockerfile:1.6
# This docker file can be used to set up a container for CPU benchmarks only
# This image has docker inside, that runs as a proper daemon.

FROM cruizba/ubuntu-dind:noble-latest
LABEL org.opencontainers.image.source https://github.com/cedana/cedana

# install packages
RUN <<EOT
set -eux
APT_PACKAGES="build-essential sudo wget git make curl libnl-3-dev libnet-dev lsof psmisc \
    pkg-config libbsd-dev runc libcap-dev libgpgme-dev iptables iproute2 \
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

# install otelcol
RUN <<EOT
curl --proto '=https' --tlsv1.2 -fOL https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.114.0/otelcol_0.114.0_linux_amd64.tar.gz
tar -xvf otelcol_0.114.0_linux_amd64.tar.gz
mv otelcol /usr/local/bin/
EOT

# install go
RUN <<EOT
set -eux
wget https://go.dev/dl/go1.23.0.linux-amd64.tar.gz && rm -rf /usr/local/go
tar -C /usr/local -xzf go1.23.0.linux-amd64.tar.gz && rm go1.23.0.linux-amd64.tar.gz
EOT
ENV PATH=${PATH}:/usr/local/go/bin:${GOPATH}/bin:/usr/local/bin

# install CRIU
# TODO: replace with own CRIU
RUN <<EOT
set -eux
wget https://download.opensuse.org/repositories/devel:/tools:/criu/xUbuntu_24.04/amd64/criu_4.0-3_amd64.deb
dpkg -i criu_4.0-3_amd64.deb
rm criu_4.0-3_amd64.deb
EOT

# register containerd daemon
RUN <<EOT
set -eux
cat <<EOF > /etc/supervisor/conf.d/containerd.conf
[program:containerd]
command=/usr/local/bin/containerd
autostart=true
autorestart=true
stderr_logfile=/var/log/containerd.err.log
stdout_logfile=/var/log/containerd.out.log
EOF
EOT

VOLUME ["/src"]
WORKDIR /src
ENV PATH=${PATH}:/src
RUN git config --global --add safe.directory `pwd`

CMD ["/bin/bash"]
