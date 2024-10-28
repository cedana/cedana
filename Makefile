GOCMD=go
GOBUILD=CGO_ENABLED=1 $(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get

BINARY=cedana
BINARY_SOURCES=$(wildcard **/*.go)
PROTO_SOURCES=$(wildcard pkg/api/proto/**/*.proto)
VERSION=$(shell git describe --tags --always)
LDFLAGS=-X main.Version=$(VERSION)

.PHONY: proto build

all: proto build

build: $(BINARY_SOURCES)
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BINARY) -v

clean:
	$(GOCLEAN)

test:
	$(GOTEST) -v ./...

proto: $(PROTO_SOURCES)
	@cd pkg/api && ./generate.sh
