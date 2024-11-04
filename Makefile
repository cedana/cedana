OUT_DIR=$(PWD)
GOCMD=go
GOBUILD=CGO_ENABLED=1 $(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get

all: proto build plugins
.PHONY: proto build plugins

############
## Cedana ##
############

BINARY=cedana
BINARY_SOURCES=$(wildcard **/*.go)
VERSION=$(shell git describe --tags --always)
LDFLAGS=-X main.Version=$(VERSION)

build: $(BINARY_SOURCES)
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY)

clean:
	$(GOCLEAN)

test:
	$(GOTEST) -v ./...

systemd:
	@echo "Installing systemd service..."

###########
## Proto ##
###########

PROTO_SOURCES=$(wildcard pkg/api/proto/**/*.proto)

proto: $(PROTO_SOURCES)
	@cd pkg/api && ./generate.sh

#############
## Plugins ##
#############

PLUGIN_SOURCES=$(wildcard plugins/**/*.go)

plugins: $(PLUGIN_SOURCES)
	for path in $(wildcard plugins/*); do \
		name=$$(basename $$path); \
		$(GOBUILD) -C $$path -buildmode=plugin -o $(OUT_DIR)/libcedana-$$name.so ;\
	done
