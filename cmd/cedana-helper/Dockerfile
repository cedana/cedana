FROM ubuntu:22.04
COPY --from=golang:1.21.1-bookworm /usr/local/go/ /usr/local/go
COPY --from=cedana/cedana:latest /usr/local/bin/cedana /usr/local/bin/cedana
ENV PATH="/usr/local/go/bin:${PATH}"

RUN apt-get update && \
    apt-get install -y software-properties-common git zip

RUN git clone https://github.com/cedana/cedana.git
WORKDIR /cedana/cmd/cedana-helper
RUN go build

ENV USER="root"
CMD ["/cedana/cmd/cedana-helper/cedana-helper"]
