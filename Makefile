OUT_DIR=$(PWD)
SCRIPTS_DIR=$(PWD)/scripts
GOCMD=go
GOBUILD=CGO_ENABLED=1 $(GOCMD) build
GOMODULE=github.com/cedana/cedana
SUDO=sudo -E env "PATH=$(PATH)"

ifndef VERBOSE
.SILENT:
endif

all: build install plugins plugins-install ## Build and install with all plugins
.PHONY: build plugins

##########
##@ Cedana
##########

BINARY=cedana
BINARY_SOURCES=$(wildcard **/*.go)
VERSION=$(shell git describe --tags --always)
LDFLAGS=-X main.Version=$(VERSION)

build: $(BINARY_SOURCES) ## Build the binary
	@echo "Building $(BINARY)..."
	$(GOCMD) mod tidy
	$(GOBUILD) -buildvcs=false -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY)

install: ## Install the binary
	@echo "Installing $(BINARY)..."
	$(SUDO) cp $(OUT_DIR)/$(BINARY) /usr/local/bin/$(BINARY)

start: ## Start the daemon
	$(SUDO) ./$(BINARY) daemon start

stop: ## Stop the daemon
	@echo "Stopping cedana..."
	$(SUDO) pkill -f $(BINARY) -TERM

start-systemd: build plugins ## Build and start the systemd daemon
	@echo "Starting systemd service..."
	$(SCRIPTS_DIR)/start-systemd.sh --plugins=runc

stop-systemd: ## Stop the systemd daemon
	@echo "Stopping systemd service..."
	if [ -f /etc/systemd/system/cedana.service ]; then \
		$(SCRIPTS_DIR)/stop-systemd.sh ;\
		$(SUDO) rm -f /etc/systemd/system/cedana.service ;\
	fi
	@echo "No systemd service found."

reset: stop-systemd stop reset-plugins reset-db reset-config reset-tmp reset-logs ## Reset (everything)
	@echo "Resetting cedana..."
	rm -f $(OUT_DIR)/$(BINARY)
	$(SUDO) rm -f /usr/local/bin/$(BINARY)

reset-db: ## Reset the local database
	@echo "Resetting database..."
	$(SUDO) rm -f /tmp/cedana*.db

reset-config: ## Reset configuration files
	@echo "Resetting configuration..."
	rm -rf ~/.cedana/*

reset-tmp: ## Reset temporary files
	@echo "Resetting temporary files..."
	$(SUDO) rm -rf /tmp/cedana*
	$(SUDO) rm -rf /tmp/dump*
	$(SUDO) rm -rf /dev/shm/cedana*

reset-logs: ## Reset logs
	@echo "Resetting logs..."
	$(SUDO) rm -rf /var/log/cedana*

###########
##@ Plugins
###########

PLUGIN_SOURCES=$(wildcard plugins/**/*.go)
plugins: $(PLUGIN_SOURCES) ## Build plugins
	for path in $(wildcard plugins/*); do \
		if [ -f $$path/*.go ]; then \
			name=$$(basename $$path); \
			echo "Building plugin $$name..."; \
			$(GOBUILD) -C $$path -buildvcs=false -ldflags "$(LDFLAGS)" -buildmode=plugin -o $(OUT_DIR)/libcedana-$$name.so ;\
		fi ;\
	done ;\

plugins-install: ## Install plugins
	@echo "Installing plugins..."
	list=""
	for path in $(wildcard plugins/*); do \
		if [ -f $$path/*.go ]; then \
			name=$$(basename $$path); \
			list="$$name $$list"; \
		fi ;\
	done ;\
	$(SUDO) $(BINARY) plugin install $$list ;\

reset-plugins: ## Reset & uninstall plugins
	@echo "Resetting plugins..."
	rm -rf $(OUT_DIR)/libcedana-*.so
	if [ -f $(OUT_DIR)/$(BINARY) ]; then \
		$(SUDO) $(BINARY) plugin remove --all ;\
	fi

###########
##@ Testing
###########

PARALLELISM?=8
BATS_CMD=bats --jobs $(PARALLELISM)

test: test-unit test-regression ## Run all tests

test-unit: ## Run unit tests (with benchmarks)
	@echo "Running unit tests..."
	$(GOCMD) test -v $(GOMODULE)/...test -bench=. -benchmem

test-regression: ## Run all regression tests (PARALLELISM=<n>)
	@echo "Running all regression tests..."
	@echo "Parallelism: $(PARALLELISM)"
	if [ -f /.dockerenv ]; then \
		make plugins-install > /dev/null ;\
		echo "Using unique instance of daemon per test..." ;\
		$(BATS_CMD) -r test/regression ;\
		echo "Using a persistent instance of daemon across tests..." ;\
		PERSIST_DAEMON=1 $(BATS_CMD) -r test/regression ;\
	else \
		$(DOCKER_TEST_RUN) make test-regression PARALLELISM=$(PARALLELISM) ;\
	fi

test-regression-cedana: ## Run regression tests for cedana
	@echo "Running regression tests for cedana..."
	@echo "Parallelism: $(PARALLELISM)"
	if [ -f /.dockerenv ]; then \
		echo "Using unique instance of daemon per test..." ;\
		$(BATS_CMD) test/regression ;\
		echo "Using a persistent instance of daemon across tests..." ;\
		PERSIST_DAEMON=1 $(BATS_CMD) test/regression ;\
	else \
		$(DOCKER_TEST_RUN) make test-regression-cedana PARALLELISM=$(PARALLELISM) ;\
	fi

test-regression-plugin: ## Run regression tests for a plugin (PLUGIN=<plugin>)
	@echo "Running regression tests for plugin $$PLUGIN..."
	@echo "Parallelism: $(PARALLELISM)"
	if [ -f /.dockerenv ]; then \
		$(SUDO) $(BINARY) plugin install $$PLUGIN > /dev/null ;\
		echo "Using unique instance of daemon per test..." ;\
		$(BATS_CMD) test/regression/plugins/$$PLUGIN.bats ;\
		echo "Using a persistent instance of daemon across tests..." ;\
		PERSIST_DAEMON=1 $(BATS_CMD) test/regression/plugins/$$PLUGIN.bats ;\
	else \
		$(DOCKER_TEST_RUN) make test-regression-plugin PLUGIN=$$PLUGIN PARALLELISM=$(PARALLELISM) ;\
	fi

##########
##@ Docker
##########

DOCKER_TEST_IMAGE=cedana/cedana-test:latest
DOCKER_TEST_RUN=docker run --privileged --init --cgroupns=private -it --rm -v $(PWD):/src:ro $(DOCKER_TEST_IMAGE)

docker-test: ## Build the test Docker image
	@echo "Building test Docker image..."
	cd test ;\
	docker build -t $(DOCKER_TEST_IMAGE) . ;\
	cd -

docker-test-push: ## Push the test Docker image
	@echo "Pushing test Docker image..."
	docker push $(DOCKER_TEST_IMAGE)

###########
##@ Utility
###########

format: ## Format all code
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

spacing=24
help:  ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\033[36m\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[34m%-$(spacing)s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
