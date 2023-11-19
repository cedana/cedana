FROM ubuntu:22.04

# Install golang
COPY --from=golang:1.21.1-bookworm /usr/local/go/ /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

# Install everything else 
RUN apt-get update && \
    apt-get install -y software-properties-common git zip && \
    rm -rf /var/lib/apt/lists/*

RUN add-apt-repository ppa:criu/ppa
RUN apt update && apt install -y criu python3 pip sudo 

RUN git clone https://github.com/cedana/cedana && mkdir ~/.cedana 
WORKDIR /cedana

ENV USER="root"
RUN go build && ./cedana bootstrap 

RUN apt-get install iptables

ENTRYPOINT /bin/bash