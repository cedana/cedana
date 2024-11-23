OUT_DIR=$(PWD)
SCRIPTS_DIR=$(PWD)/scripts
GOCMD=go
GOBUILD=CGO_ENABLED=1 $(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get

all: build plugins
.PHONY: build plugins

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

start-daemon: build
	@sudo ./$(BINARY) daemon start

start-systemd: build
	@echo "Installing systemd service..."
	$(SCRIPTS_DIR)/start-systemd.sh --plugins=runc

#############
## Plugins ##
#############

PLUGIN_SOURCES=$(wildcard plugins/**/*.go)

plugins: $(PLUGIN_SOURCES)
	for path in $(wildcard plugins/*); do \
		if [ -f $$path/*.go ]; then \
			name=$$(basename $$path); \
			$(GOBUILD) -C $$path -ldflags "$(LDFLAGS)" -buildmode=plugin -o $(OUT_DIR)/libcedana-$$name.so ;\
			sudo $(BINARY) plugin install $$name ;\
		fi ;\
	done
