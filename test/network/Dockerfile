FROM ubuntu:22.04

# Install golang
COPY --from=golang:1.21.1-bookworm /usr/local/go/ /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

# Install everything else 
RUN apt-get update && \
    apt-get install -y software-properties-common git zip && \
    rm -rf /var/lib/apt/lists/*

RUN add-apt-repository ppa:criu/ppa
RUN apt update && apt install -y criu python3 pip apt-transport-https ca-certificates gnupg curl sudo


RUN git clone https://github.com/cedana/cedana && mkdir ~/.cedana 
WORKDIR /cedana
RUN pip install -r test/benchmarks/requirements


ENV USER="root"
RUN go build && ./cedana bootstrap 

# set up bq cli 
RUN curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key --keyring /usr/share/keyrings/cloud.google.gpg add -
RUN apt-get update && sudo apt-get install google-cloud-cli

ENV GOOGLE_APPLICATION_CREDENTIALS=${GOOGLE_APPLICATION_CREDENTIALS}
ENV CEDANA_PROFILING_ENABLED=true
CMD python3 test/benchmarks/benchmarks.py
