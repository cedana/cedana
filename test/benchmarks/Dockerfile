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

ENV USER="root"
RUN go build && ./cedana bootstrap 

# force leave_running=false for benchmarks 
RUN echo '{"client":{"leave_running":false,"signal_process_pre_dump":false,"signal_process_timeout":0,"task":""}}' > ~/.cedana/client_config.json

## set up bq env and run benchmarks
ENV CEDANA_PROFILING_ENABLED=true 
ENV CEDANA_REMOTE=true
RUN cd test/benchmarks pip install -r requirements 

RUN mkdir benchmark_results
## have to force protobuf compilation with older version
RUN pip install protobuf==3.20.1 requests psutil google-cloud-bigquery torch
RUN cd test/benchmarks && protoc --python_out=. profile.proto

## start daemon and benchmark 
CMD ["sh", "-c", "./cedana daemon start && python3 test/benchmarks/benchmark.py"]