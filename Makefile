OUT_DIR=$(PWD)
SCRIPTS_DIR=$(PWD)/scripts
GOCMD=go
GOBUILD=CGO_ENABLED=1 $(GOCMD) build

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

build: $(BINARY_SOURCES) ## Build the binary
	@echo "Building $(BINARY)..."
	$(GOCMD) mod tidy
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY)

start: build plugins ## Build and start the daemon
	@sudo ./$(BINARY) daemon start

stop: ## Stop the daemon
	@echo "Stopping cedana..."
	@sudo pkill -f $(BINARY) -TERM

start-systemd: build plugins ## Build and start the daemon using systemd
	@echo "Starting systemd service..."
	$(SCRIPTS_DIR)/start-systemd.sh --plugins=runc

stop-systemd: ## Stop the systemd daemon
	@echo "Stopping systemd service..."
	if [ -f /etc/systemd/system/cedana.service ]; then \
		$(SCRIPTS_DIR)/stop-systemd.sh ;\
		@sudo rm -f /etc/systemd/system/cedana.service ;\
	fi
	@echo "No systemd service found."

reset: stop-systemd stop reset-plugins reset-db reset-config reset-tmp reset-logs ## Reset cedana (everything)
	@echo "Resetting cedana..."
	rm -rf $(OUT_DIR)/$(BINARY)

reset-db: ## Reset the local database
	@echo "Resetting database..."
	@sudo rm -f /tmp/cedana.db

reset-config: ## Reset configuration files
	@echo "Resetting configuration..."
	rm -rf ~/.cedana/*

reset-tmp: ## Reset temporary files
	@echo "Resetting temporary files..."
	@sudo rm -rf /tmp/cedana-*
	@sudo rm -rf /tmp/dump-*
	@sudo rm -rf /dev/shm/cedana*

reset-logs: ## Reset logs
	@echo "Resetting logs..."
	sudo rm -rf /var/log/cedana*

#############
## Plugins ##
#############

PLUGIN_SOURCES=$(wildcard plugins/**/*.go)
plugins: $(PLUGIN_SOURCES) ## Build & install plugins
	list=""
	for path in $(wildcard plugins/*); do \
		if [ -f $$path/*.go ]; then \
			name=$$(basename $$path); \
			list="$$name $$list"; \
			echo "Building plugin $$name..."; \
			$(GOBUILD) -C $$path -ldflags "$(LDFLAGS)" -buildmode=plugin -o $(OUT_DIR)/libcedana-$$name.so ;\
		fi ;\
	done ;\
	sudo $(BINARY) plugin install $$list ;\

reset-plugins: ## Reset plugins
	@echo "Resetting plugins..."
	rm -rf $(OUT_DIR)/libcedana-*.so
	if [ -p $(OUT_DIR)/$(BINARY) ]; then \
		sudo $(BINARY) plugin remove --all ;\
	fi

#############
## Testing ##
#############

test: test-go ## Run all tests

test-go: ## Run go tests
	@echo "Running go tests..."
	$(GOCMD) test -v ./...

test-regression: ## Run regression tests
	@echo "Running regression tests..."
	# TODO: Implement regression tests

#############
## Helpers ##
#############

##@ Utility
help:  ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
