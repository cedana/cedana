PWD=$(shell pwd)
OUT_DIR=$(PWD)
SCRIPTS_DIR=$(PWD)/scripts
GOCMD=go
GOBUILD=CGO_ENABLED=1 $(GOCMD) build
GOMODULE=github.com/cedana/cedana
SUDO=sudo -E env "PATH=$(PATH)"

DEBUG_FLAGS=-gcflags="all=-N -l" -ldflags "-compressdwarf=false"

ifndef VERBOSE
.SILENT:
endif

all: build install plugins plugins-install ## Build and install (with all plugins)

##########
##@ Cedana
##########

BINARY=cedana
BINARY_SOURCES=$(shell find . -path ./test -prune -o -type f -name '*.go' -not -path './plugins/*' -print)
INSTALL_PATH=/usr/local/bin/cedana
VERSION=$(shell git describe --tags --always)
LDFLAGS=-X main.Version=$(VERSION)

build: $(BINARY)

$(BINARY): $(BINARY_SOURCES) ## Build the binary
	@echo "Building $(BINARY)..."
	$(GOCMD) mod tidy
	$(GOBUILD) -buildvcs=true -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY)

debug: $(BINARY_SOURCES) ## Build the binary with debug symbols and no optimizations
	@echo "Building $(BINARY) with debug symbols..."
	$(GOCMD) mod tidy
	$(GOBUILD) -buildvcs=true $(DEBUG_FLAGS) -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(BINARY)

install: $(INSTALL_PATH) ## Install the binary

$(INSTALL_PATH): $(BINARY) ## Install the binary
	@echo "Installing $(BINARY)..."
	$(SUDO) cp $(OUT_DIR)/$(BINARY) $(INSTALL_PATH)

start: $(INSTALL_PATH) ## Start the daemon
	$(SUDO) $(BINARY) daemon start

install-systemd: install ## Install the systemd daemon
	@echo "Installing systemd service..."
	$(SUDO) $(SCRIPTS_DIR)/host/systemd-install.sh

reset-systemd: ## Reset the systemd daemon
	@echo "Stopping systemd service..."
	$(SUDO) $(SCRIPTS_DIR)/host/systemd-reset.sh ;\
	sleep 1

reset: reset-systemd reset-plugins reset-db reset-config reset-tmp reset-logs ## Reset (everything)
	@echo "Resetting cedana..."
	rm -f $(OUT_DIR)/$(BINARY)
	$(SUDO) rm -f $(INSTALL_PATH)

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
	$(SUDO) rm -rf /run/cedana*

reset-logs: ## Reset logs
	@echo "Resetting logs..."
	$(SUDO) rm -rf /var/log/cedana*

###########
##@ Plugins
###########

PLUGIN_SOURCES=$(shell find plugins -name '*.go')
PLUGIN_BINARIES=$(shell ls plugins | sed 's/^/.\/libcedana-/g' | sed 's/$$/.so/g')
PKG_SOURCES=$(shell find pkg plugins -name '*.go')
PLUGIN_INSTALL_PATHS=$(shell ls plugins | sed 's/^/\/usr\/local\/lib\/libcedana-/g' | sed 's/$$/.so/g')

plugin: ## Build a plugin (PLUGIN=<plugin>)
	@echo "Building plugin $$PLUGIN..."
	$(GOBUILD) -C plugins/$$PLUGIN -buildvcs=true -ldflags "$(LDFLAGS)" -buildmode=plugin -o $(OUT_DIR)/libcedana-$$PLUGIN.so

plugin-debug: ## Build a plugin with debug symbols and no optimizations (PLUGIN=<plugin>)
	@echo "Building plugin $$PLUGIN with debug symbols..."
	$(GOBUILD) -C plugins/$$PLUGIN -buildvcs=true $(DEBUG_FLAGS) -ldflags "$(LDFLAGS)" -buildmode=plugin -o $(OUT_DIR)/libcedana-$$PLUGIN.so

plugin-install: plugin ## Install a plugin (PLUGIN=<plugin>)
	@echo "Installing plugin $$PLUGIN..."
	$(SUDO) cp $(OUT_DIR)/libcedana-$$PLUGIN.so /usr/local/lib

plugins: $(PLUGIN_BINARIES) ## Build all plugins

plugins-debug: ## Build all plugins with debug symbols
	for path in $(wildcard plugins/*); do \
		if [ -f $$path/*.go ]; then \
			name=$$(basename $$path); \
			echo "Building plugin $$name with debug symbols..."; \
			$(GOBUILD) -C $$path -buildvcs=true $(DEBUG_FLAGS) -ldflags "$(LDFLAGS)" -buildmode=plugin -o $(OUT_DIR)/libcedana-$$name.so ;\
		fi ;\
	done ;\

$(PLUGIN_BINARIES): $(PLUGIN_SOURCES) $(PKG_SOURCES)
	for path in $(wildcard plugins/*); do \
		if [ -f $$path/*.go ]; then \
			name=$$(basename $$path); \
			echo "Building plugin $$name..."; \
			$(GOBUILD) -C $$path -buildvcs=true -ldflags "$(LDFLAGS)" -buildmode=plugin -o $(OUT_DIR)/libcedana-$$name.so ;\
		fi ;\
	done ;\

plugins-install: $(PLUGIN_INSTALL_PATHS) ## Install all plugins

$(PLUGIN_INSTALL_PATHS): $(PLUGIN_BINARIES)
	for path in $(wildcard plugins/*); do \
		if [ -f $$path/*.go ]; then \
			name=$$(basename $$path); \
			echo "Installing plugin $$name..."; \
			$(SUDO) cp $(OUT_DIR)/libcedana-$$name.so /usr/local/lib ;\
		fi ;\
	done ;\

reset-plugins: ## Reset & uninstall plugins
	@echo "Resetting plugins..."
	rm -rf $(OUT_DIR)/libcedana-*.so
	$(SUDO) rm -rf /usr/local/lib/*cedana*
	$(SUDO) rm -rf /usr/local/bin/*cedana*

# All-in-one debug target
all-debug: debug install plugins-debug plugins-install ## Build and install with debug symbols (all components)

###########
##@ Testing
###########

PARALLELISM?=8
TAGS?=
ARGS?=
TIMEOUT?=600
RETRIES?=0
DEBUG?=0
HELPER_REPO?=cedana/cedana-helper
HELPER_TAG?=""
HELPER_DIGEST?=""
CONTROLLER_REPO?=cedana/cedana-controller
CONTROLLER_TAG?=""
CONTROLLER_DIGEST?=""
HELM_CHART?=""
BATS_CMD_TAGS=BATS_TEST_TIMEOUT=$(TIMEOUT) BATS_TEST_RETRIES=$(RETRIES) bats --timing \
				--filter-tags $(TAGS) --jobs $(PARALLELISM) $(ARGS) --print-output-on-failure \
				--output /tmp --report-formatter junit
BATS_CMD=BATS_TEST_TIMEOUT=$(TIMEOUT) BATS_TEST_RETRIES=$(RETRIES) bats --timing \
		        --jobs $(PARALLELISM) $(ARGS) --print-output-on-failure \
				--output /tmp --report-formatter junit

test: test-unit test-regression test-k8s ## Run all tests (PARALLELISM=<n>, GPU=[0|1], TAGS=<tags>, TIMEOUT=<timeout>, RETRIES=<retries>, DEBUG=[0|1])

test-unit: ## Run unit tests (with benchmarks)
	@echo "Running unit tests..."
	$(GOCMD) test -v $(GOMODULE)/...test -bench=. -benchmem

test-regression: ## Run regression tests (PARALLELISM=<n>, GPU=[0|1], TAGS=<tags>, TIMEOUT=<timeout>, RETRIES=<retries>, DEBUG=[0|1])
	if [ -f /.dockerenv ]; then \
		echo "Running regression tests..." ;\
		echo "Parallelism: $(PARALLELISM)" ;\
		echo "Using unique instance of daemon per test..." ;\
		if [ "$(TAGS)" = "" ]; then \
			$(BATS_CMD) -r test/regression ; status_isolated=$$? ;\
		else \
			$(BATS_CMD_TAGS) -r test/regression ; status_isolated=$$? ;\
		fi ;\
		if [ -f /tmp/report.xml ]; then \
			mv /tmp/report.xml /tmp/report-isolated.xml ;\
		fi ;\
		echo "Using a persistent instance of daemon across tests..." ;\
		if [ "$(TAGS)" = "" ]; then \
			PERSIST_DAEMON=1 $(BATS_CMD) -r test/regression ; status_persistent=$$? ;\
		else \
			PERSIST_DAEMON=1 $(BATS_CMD_TAGS) -r test/regression ; status_persistent=$$? ;\
		fi ;\
		if [ -f /tmp/report.xml ]; then \
			mv /tmp/report.xml /tmp/report-persistent.xml ;\
		fi ;\
		if [ $$status_isolated -ne 0 ]; then \
			echo "Isolated tests failed" ;\
			exit $$status_isolated ;\
		elif [ $$status_persistent -ne 0 ]; then \
			echo "Persistent tests failed" ;\
			exit $$status_persistent ;\
		else \
			echo "All tests passed!" ;\
		fi ;\
	else \
		if [ "$(GPU)" = "1" ]; then \
			echo "Running in container $(DOCKER_TEST_IMAGE_CUDA)..." ;\
			$(DOCKER_TEST_CREATE_CUDA) ;\
			$(DOCKER_TEST_START) ;\
			$(DOCKER_TEST_EXEC) make test-regression \
				ARGS=$(ARGS) \
				PARALLELISM=$(PARALLELISM) \
				TAGS=$(TAGS) \
				TIMEOUT=$(TIMEOUT) \
				RETRIES=$(RETRIES) \
				DEBUG=$(DEBUG) ;\
			$(DOCKER_TEST_REMOVE) ;\
		else \
			echo "Running in container $(DOCKER_TEST_IMAGE)..." ;\
			$(DOCKER_TEST_CREATE) ;\
			$(DOCKER_TEST_START) ;\
			$(DOCKER_TEST_EXEC) make test-regression \
				ARGS=$(ARGS) \
				PARALLELISM=$(PARALLELISM) \
				GPU=$(GPU) \
				TAGS=$(TAGS) \
				TIMEOUT=$(TIMEOUT) \
				RETRIES=$(RETRIES) \
				DEBUG=$(DEBUG) ;\
			$(DOCKER_TEST_REMOVE) ;\
		fi ;\
	fi

test-k8s: ## Run kubernetes e2e tests (PARALLELISM=<n>, GPU=[0|1], TAGS=<tags>, TIMEOUT=<timeout>, RETRIES=<retries>, DEBUG=[0|1], CONTROLLER_REPO=<repo>, CONTROLLER_TAG=<tag>, CONTROLLER_DIGEST=<digest>, HELPER_REPO=<repo>, HELPER_TAG=<tag>, HELPER_DIGEST=<digest>, HELM_CHART=<path|version>)
	if [ -f /.dockerenv ]; then \
		echo "Running kubernetes e2e tests..." ;\
		echo "Parallelism: $(PARALLELISM)" ;\
		if [ "$(TAGS)" = "" ]; then \
			$(BATS_CMD) -r test/k8s ; status=$$? ;\
		else \
			$(BATS_CMD_TAGS) -r test/k8s ; status=$$? ;\
		fi ;\
		if [ $$status -ne 0 ]; then \
			echo "Kubernetes e2e tests failed" ;\
			exit $$status ;\
		else \
			echo "All kubernetes e2e tests passed!" ;\
		fi ;\
	else \
        MAKE_ADDITIONAL_OPTS=""; \
        if [ -n "$(HELM_CHART)" ]; then \
			if echo "$(HELM_CHART)" | grep -q /; then \
				MAKE_ADDITIONAL_OPTS="HELM_CHART=/helm-chart"; \
			else \
				MAKE_ADDITIONAL_OPTS="HELM_CHART=$(HELM_CHART)"; \
			fi; \
        fi; \
		if [ "$(GPU)" = "1" ]; then \
			echo "Running in container $(DOCKER_TEST_IMAGE_CUDA)..." ;\
			$(DOCKER_TEST_CREATE_CUDA) ;\
			$(DOCKER_TEST_START) ;\
			$(DOCKER_TEST_EXEC) make test-k8s \
				ARGS=$(ARGS) \
				PARALLELISM=$(PARALLELISM) \
				TAGS=$(TAGS) \
				TIMEOUT=$(TIMEOUT) \
				RETRIES=$(RETRIES) \
				DEBUG=$(DEBUG) \
				CONTROLLER_REPO=$(CONTROLLER_REPO) \
				CONTROLLER_TAG=$(CONTROLLER_TAG) \
				CONTROLLER_DIGEST=$(CONTROLLER_DIGEST) \
				HELPER_REPO=$(HELPER_REPO) \
				HELPER_TAG=$(HELPER_TAG) \
				HELPER_DIGEST=$(HELPER_DIGEST) \
				$$MAKE_ADDITIONAL_OPTS ;\
			$(DOCKER_TEST_REMOVE) ;\
		else \
			echo "Running in container $(DOCKER_TEST_IMAGE)..." ;\
			$(DOCKER_TEST_CREATE) ;\
			$(DOCKER_TEST_START) ;\
			$(DOCKER_TEST_EXEC) make test-k8s \
				ARGS=$(ARGS) \
				PARALLELISM=$(PARALLELISM) \
				GPU=$(GPU) \
				TAGS=$(TAGS) \
				TIMEOUT=$(TIMEOUT) \
				RETRIES=$(RETRIES) \
				DEBUG=$(DEBUG) \
				CONTROLLER_REPO=$(CONTROLLER_REPO) \
				CONTROLLER_TAG=$(CONTROLLER_TAG) \
				CONTROLLER_DIGEST=$(CONTROLLER_DIGEST) \
				HELPER_REPO=$(HELPER_REPO) \
				HELPER_TAG=$(HELPER_TAG) \
				HELPER_DIGEST=$(HELPER_DIGEST) \
				$$MAKE_ADDITIONAL_OPTS ;\
			$(DOCKER_TEST_REMOVE) ;\
		fi ;\
	fi

test-enter: ## Enter the test environment
	$(DOCKER_TEST_CREATE) ;\
	if [ "$$?" -ne 0 ]; then \
		$(DOCKER_TEST_EXEC) /bin/bash ;\
	else \
		$(DOCKER_TEST_START) ;\
		$(DOCKER_TEST_EXEC) /bin/bash ;\
		$(DOCKER_TEST_REMOVE) ;\
	fi ;\

test-enter-cuda: ## Enter the test environment (CUDA)
	$(DOCKER_TEST_CREATE_CUDA) ;\
	if [ "$$?" -ne 0 ]; then \
		$(DOCKER_TEST_EXEC) /bin/bash ;\
	else \
		$(DOCKER_TEST_START) ;\
		$(DOCKER_TEST_EXEC) /bin/bash ;\
		$(DOCKER_TEST_REMOVE) ;\
	fi ;\

test-k9s: ## Enter k9s in the test environment
	$(DOCKER_TEST_EXEC) k9s ;\

##########
##@ Docker
##########

# The docker container used as the test environment will have access to the locally installed plugin libraries
# and binaries, *if* they are installed in /usr/local/lib and /usr/local/bin respectively (which is
# the default).

DOCKER_IMAGE=cedana/cedana-helper:latest
DOCKER_TEST_CONTAINER_NAME=cedana-test
DOCKER_TEST_IMAGE=cedana/cedana-test:latest
DOCKER_TEST_IMAGE_CUDA=cedana/cedana-test:cuda
DOCKER_TEST_START=docker start $(DOCKER_TEST_CONTAINER_NAME) >/dev/null
DOCKER_TEST_EXEC=docker exec -it $(DOCKER_TEST_CONTAINER_NAME)
DOCKER_TEST_REMOVE=docker rm -f $(DOCKER_TEST_CONTAINER_NAME) >/dev/null
PLATFORM=linux/amd64,linux/arm64

PLUGIN_LIB_COPY=find /usr/local/lib -type f -name '*cedana*' -not -name '*gpu*' -exec docker cp {} $(DOCKER_TEST_CONTAINER_NAME):{} \; >/dev/null
PLUGIN_BIN_COPY=find /usr/local/bin -type f -name '*cedana*' -not -name '*gpu*' -exec docker cp {} $(DOCKER_TEST_CONTAINER_NAME):{} \; >/dev/null
PLUGIN_LIB_COPY_GPU=find /usr/local/lib -type f -name '*cedana*' -and -name '*gpu*' -exec docker cp {} $(DOCKER_TEST_CONTAINER_NAME):{} \; >/dev/null
PLUGIN_BIN_COPY_GPU=find /usr/local/bin -type f -name '*cedana*' -and -name '*gpu*' -exec docker cp {} $(DOCKER_TEST_CONTAINER_NAME):{} \; >/dev/null
PLUGIN_BIN_COPY_CRIU=find /usr/local/bin -type f -name 'criu' -exec docker cp {} $(DOCKER_TEST_CONTAINER_NAME):{} \; >/dev/null
HELM_CHART_COPY=if [ -n "$$HELM_CHART" ]; then docker cp $(HELM_CHART) $(DOCKER_TEST_CONTAINER_NAME):/helm-chart ; fi >/dev/null

DOCKER_TEST_CREATE_OPTS=--privileged --init --cgroupns=host --name=$(DOCKER_TEST_CONTAINER_NAME) \
						-v $(PWD):/src:ro -v /var/run/docker.sock:/var/run/docker.sock \
				-e CEDANA_URL=$(CEDANA_URL) -e CEDANA_AUTH_TOKEN=$(CEDANA_AUTH_TOKEN) -e HF_TOKEN=$(HF_TOKEN) \
				-e AWS_ACCESS_KEY_ID=$(AWS_ACCESS_KEY_ID) -e AWS_SECRET_ACCESS_KEY=$(AWS_SECRET_ACCESS_KEY) -e AWS_REGION=$(AWS_REGION) \
				$(DOCKER_ADDITIONAL_OPTS)
DOCKER_TEST_CREATE=docker create $(DOCKER_TEST_CREATE_OPTS) $(DOCKER_TEST_IMAGE) sleep inf >/dev/null && \
						$(PLUGIN_LIB_COPY) && \
						$(PLUGIN_BIN_COPY) && \
						$(PLUGIN_BIN_COPY_CRIU) && \
						$(HELM_CHART_COPY) >/dev/null
DOCKER_TEST_CREATE_CUDA=docker create --gpus=all --ipc=host $(DOCKER_TEST_CREATE_OPTS) $(DOCKER_TEST_IMAGE_CUDA) sleep inf >/dev/null && \
						$(PLUGIN_LIB_COPY) && \
						$(PLUGIN_BIN_COPY) && \
						$(PLUGIN_LIB_COPY_GPU) && \
						$(PLUGIN_BIN_COPY_GPU) && \
						$(PLUGIN_BIN_COPY_CRIU) >/dev/null

docker: ## Build the helper Docker image (PLATFORM=linux/amd64,linux/arm64)
	@echo "Building helper Docker image..."
	docker buildx build --platform $(PLATFORM) -t $(DOCKER_IMAGE) --load . ;\

docker-test: ## Build the test Docker image (PLATFORM=linux/amd64,linux/arm64)
	@echo "Building test Docker image..."
	cd test ;\
	docker buildx build --platform $(PLATFORM) -t $(DOCKER_TEST_IMAGE) --load . ;\
	cd -

docker-test-cuda: ## Build the test Docker image for CUDA (PLATFORM=linux/amd64,linux/arm64)
	@echo "Building test CUDA Docker image..."
	cd test ;\
	docker buildx build --platform $(PLATFORM) -t $(DOCKER_TEST_IMAGE_CUDA) -f Dockerfile.cuda --load . ;\
	cd -

docker-test-push: ## Push the test Docker image (PLATFORM=linux/amd64,linux/arm64)
	@echo "Pushing test Docker image..."
	docker push $(DOCKER_TEST_IMAGE)

docker-test-cuda-push: ## Push the test Docker image for CUDA (PLATFORM=linux/amd64,linux/arm64)
	@echo "Pushing test CUDA Docker image..."
	docker push $(DOCKER_TEST_IMAGE_CUDA)

###########
##@ Utility
###########

format: ## Format all code
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

spacing=24
help:  ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\033[36m\033[0m\n"} /^[a-zA-Z1-9_-]+:.*?##/ { printf "  \033[34m%-$(spacing)s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
