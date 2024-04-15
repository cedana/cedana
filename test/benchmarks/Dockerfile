# clone our repo 
FROM ubuntu:22.04

# Install golang
COPY --from=golang:1.21.1-bookworm /usr/local/go/ /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

# Install everything else 
RUN apt-get update && \
    apt-get install -y software-properties-common git zip protobuf-compiler vim wget curl && \
    rm -rf /var/lib/apt/lists/*

RUN add-apt-repository ppa:criu/ppa
RUN apt update && apt install -y criu python3 pip sudo 

RUN git clone https://github.com/cedana/cedana && mkdir ~/.cedana
WORKDIR /cedana 

ENV USER="root"
## set up bq env and run benchmarks
ENV CEDANA_OTEL_ENABLED=true 
ENV CEDANA_REMOTE=true
ENV SIGNOZ_ACCESS_TOKEN=""
RUN cd test/benchmarks pip install -r requirements 

RUN mkdir benchmark_results
## have to force protobuf compilation with older version
RUN pip install protobuf==3.20.1 requests psutil google-cloud-bigquery torch numpy
RUN cd test/benchmarks && protoc --python_out=. profile.proto

RUN git fetch 

RUN go build && ./cedana bootstrap 
RUN curl -fsSL https://code-server.dev/install.sh | sh

RUN wget https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.94.0/otelcol-contrib_0.94.0_linux_amd64.deb 
## want to bypass systemd init scripts
RUN dpkg-deb -x otelcol-contrib_0.94.0_linux_amd64.deb extracted/ && cp extracted/usr/bin/otelcol-contrib /usr/bin/otelcol-contrib

ENTRYPOINT ["./test/benchmarks/entrypoint.sh"]
