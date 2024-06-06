FROM ubuntu:22.04

# Install golang
COPY --from=golang:1.21.1-bookworm /usr/local/go/ /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

# Install everything else
RUN apt-get update && \
    apt-get install -y software-properties-common git zip && \
    rm -rf /var/lib/apt/lists/*

RUN add-apt-repository ppa:criu/ppa
RUN apt update && apt install -y criu python3 pip sudo iptables

RUN git clone https://github.com/cedana/cedana && mkdir ~/.cedana
WORKDIR /cedana

RUN git fetch --all --tags && \
    LATEST_TAG=$(git describe --tags `git rev-list --tags --max-count=1`) && \
    git checkout $LATEST_TAG

ENV USER="root"
RUN go build
RUN cp cedana /usr/local/bin/cedana

ENTRYPOINT cedana k8s-helper setup-host true
