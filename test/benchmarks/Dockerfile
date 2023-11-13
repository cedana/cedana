# clone our repo 
FROM ubuntu:22.04

# Install golang
COPY --from=golang:1.21.1-bookworm /usr/local/go/ /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

# Install everything else 
RUN apt-get update && \
    apt-get install -y software-properties-common git zip protobuf-compiler && \
    rm -rf /var/lib/apt/lists/*

RUN add-apt-repository ppa:criu/ppa
RUN apt update && apt install -y criu python3 pip sudo 

RUN git clone https://github.com/cedana/cedana && mkdir ~/.cedana 
WORKDIR /cedana 

RUN git checkout fix/dockerfile-update
RUN pip install -r test/benchmarks/requirements


ENV USER="root"
RUN go build && ./cedana bootstrap 

## set up bq env and run benchmarks
ENV GOOGLE_APPLICATION_CREDENTIALS=${GOOGLE_APPLICATION_CREDENTIALS}

RUN mkdir benchmark_results
RUN cd test/benchmarks && protoc --python_out=. profile.proto
RUN python3 test/benchmarks/benchmark.py

ENTRYPOINT /bin/bash