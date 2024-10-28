GOCMD=go
GOBUILD=CGO_ENABLED=1 $(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get

VERSION=$(shell git describe --tags --always)
LDFLAGS=-X main.Version=$(VERSION)

all: test build

build:
	$(GOBUILD) -ldflags "$(LDFLAGS)" -v

clean:
	$(GOCLEAN)

test:
	$(GOTEST) -v ./...
