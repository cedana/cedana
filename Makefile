OUT_DIR=$(PWD)
SCRIPTS_DIR=$(PWD)/scripts
GOCMD=go
GOMODTIDY=$(GOCMD) mod tidy
GOBUILD=CGO_ENABLED=1 $(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get

ifndef VERBOSE
.SILENT:
endif

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
	@echo "Building $(BINARY)..."
	$(GOMODTIDY)
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
	list="runc gpu"
	for path in $(wildcard plugins/*); do \
		if [ -f $$path/*.go ]; then \
			name=$$(basename $$path); \
			list="$$name $$list"; \
			echo "Building plugin $$name..."; \
			$(GOBUILD) -C $$path -ldflags "$(LDFLAGS)" -buildmode=plugin -o $(OUT_DIR)/libcedana-$$name.so ;\
		fi ;\
	done ;\
	sudo $(BINARY) plugin install $$list ;\
