FROM ubuntu:22.04

# Install essential packages
RUN apt-get update && \
    apt-get install -y software-properties-common git wget zip && \
    rm -rf /var/lib/apt/lists/*

# copy from github to dockerfile
ARG VERSION
RUN STRIPPED_VERSION=$(echo $VERSION | sed 's/^v//') && \
    wget "https://github.com/cedana/cedana/releases/download/${VERSION}/cedana_${STRIPPED_VERSION}_linux_amd64.tar.gz" -O /tmp/cedana.tar.gz

RUN tar -xzvf /tmp/cedana.tar.gz -C /usr/local/bin/ && rm /tmp/cedana.tar.gz


ENV USER="root"

ENTRYPOINT ["/bin/bash"]
