# clone our repo 
FROM ubuntu:22.04

# Install golang
COPY --from=golang:1.21.1-bookworm /usr/local/go/ /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"


# Install everything else 
RUN apt-get update && \
    apt-get install -y software-properties-common git && \
    rm -rf /var/lib/apt/lists/*

RUN add-apt-repository ppa:criu/ppa
RUN apt update && apt install -y criu python3 pip sudo 



RUN git clone https://github.com/cedana/cedana
WORKDIR /cedana
RUN go build 
RUN pip install -r test/benchmarks/requirements


ARG DOCKER_USER=default_user
RUN addgroup -S $DOCKER_USER && adduser -S $DOCKER_USER -G $DOCKER_USER
USER $DOCKER_USER

ENTRYPOINT /bin/bash