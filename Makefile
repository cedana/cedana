PWD=$(shell pwd)
OUT_DIR=$(PWD)
INSTALL_BIN_DIR=/usr/local/bin
INSTALL_LIB_DIR=/usr/local/lib
SCRIPTS_DIR=$(PWD)/scripts
GOCMD=go
GOBUILD=CGO_ENABLED=1 $(GOCMD) build
GOMODULE=github.com/cedana/cedana
SUDO=sudo -E env "PATH=$(PATH)"

DEBUG_FLAGS=-gcflags="all=-N -l" -ldflags "-compressdwarf=false"

ifndef VERBOSE
.SILENT:
endif

all: cedana install plugins plugins-install ## Build and install (with all plugins)

##########
##@ Cedana
##########

BINARY=cedana
BINARY_SOURCES=$(shell find . -path ./test -prune -o -type f -name '*.go' -not -path './plugins/*' -print)
PKG_SOURCES=$(sort $(shell find pkg -name '*.go'))
GO_MOD_FILES=go.sum go.mod
VERSION=$(shell git describe --tags --always)
LDFLAGS=-X main.Version=$(VERSION)
DEBUG?=0

cedana: $(OUT_DIR)/$(BINARY) ## Build the binary (DEBUG=[0|1])
$(OUT_DIR)/$(BINARY): $(BINARY_SOURCES) $(GO_MOD_FILES)
	if [ "$(DEBUG)" = "1" ]; then \
		echo "Building $(BINARY) with debug symbols..." ;\
		$(GOBUILD) -buildvcs=true $(DEBUG_FLAGS) -ldflags "$(LDFLAGS)" -o $@ ;\
	else \
		echo "Building $(BINARY)..." ;\
		$(GOBUILD) -buildvcs=true -ldflags "$(LDFLAGS)" -o $@ ;\
	fi

install: $(INSTALL_BIN_DIR)/$(BINARY) ## Install the binary
$(INSTALL_BIN_DIR)/$(BINARY): $(OUT_DIR)/$(BINARY)
	@echo "Installing $(BINARY)..."
	$(SUDO) cp $(OUT_DIR)/$(BINARY) $@

start: $(INSTALL_BIN_DIR)/$(BINARY) ## Start the daemon
	$(SUDO) $(BINARY) daemon start

install-systemd: $(INSTALL_BIN_DIR)/$(BINARY) ## Install the systemd daemon
	@echo "Installing systemd service..."
	$(SUDO) $(SCRIPTS_DIR)/host/systemd-install.sh

reset-systemd: ## Reset the systemd daemon
	@echo "Stopping systemd service..."
	$(SUDO) $(SCRIPTS_DIR)/host/systemd-reset.sh ;\
	sleep 1

reset: reset-systemd reset-plugins reset-db reset-config reset-tmp reset-logs ## Reset (everything)
	@echo "Resetting cedana..."
	$(SUDO) pkill $(BINARY) || true
	rm -f $(OUT_DIR)/$(BINARY)
	$(SUDO) rm -f $(INSTALL_BIN_DIR)/$(BINARY)

reset-db: ## Reset the local database
	@echo "Resetting database..."
	$(SUDO) rm -f /tmp/cedana*.db

reset-config: ## Reset configuration files
	@echo "Resetting configuration..."
	rm -rf ~/.cedana

reset-tmp: ## Reset temporary files
	@echo "Resetting temporary files..."
	$(SUDO) rm -rf /tmp/*cedana*
	$(SUDO) rm -rf /tmp/*dump*
	$(SUDO) rm -rf /dev/shm/*cedana*
	$(SUDO) rm -rf /run/*cedana*

reset-logs: ## Reset logs
	@echo "Resetting logs..."
	$(SUDO) rm -rf /var/log/cedana*

###########
##@ Plugins
###########

PLUGIN_NAMES=$(shell ls plugins)
PLUGIN_BINARIES=$(patsubst %,$(OUT_DIR)/libcedana-%.so,$(PLUGIN_NAMES))
PLUGIN_INSTALL_PATHS=$(patsubst %,$(INSTALL_LIB_DIR)/libcedana-%.so,$(PLUGIN_NAMES))

plugins: $(PLUGIN_BINARIES) ## Build all plugins (DEBUG=[0|1])
$(OUT_DIR)/libcedana-%.so: plugins/%/**/* plugins/%/* $(PKG_SOURCES) $(GO_MOD_FILES)
	if [ "$(DEBUG)" = "1" ]; then \
		echo "Building plugin $* with debug symbols..." ;\
		$(GOBUILD) -C plugins/"$*" -buildvcs=true $(DEBUG_FLAGS) -ldflags "$(LDFLAGS)" -buildmode=plugin -o $@ ;\
	else \
		echo "Building plugin $*..." ;\
		$(GOBUILD) -C plugins/"$*" -buildvcs=true -ldflags "$(LDFLAGS)" -buildmode=plugin -o $@ ;\
	fi

plugins-install: $(PLUGIN_INSTALL_PATHS) ## Install all plugins
$(INSTALL_LIB_DIR)/libcedana-%.so: $(OUT_DIR)/libcedana-%.so
	@echo "Installing plugin $*..."
	$(SUDO) cp $< $@

reset-plugins: ## Reset & uninstall plugins
	@echo "Resetting plugins..."
	rm -rf $(OUT_DIR)/libcedana-*.so
	$(SUDO) rm -rf $(INSTALL_LIB_DIR)/*cedana*
	$(SUDO) rm -rf $(INSTALL_BIN_DIR)/*cedana*

###########
##@ Testing
###########

PARALLELISM?=8
TAGS?=
ARGS?=
RETRIES?=0
GPU?=0
PROVIDER?=K3s
SKIP_HELM?=0
HELPER_REPO?=
HELPER_TAG?=""
HELPER_DIGEST?=""
CONTROLLER_REPO?=
CONTROLLER_TAG?=""
CONTROLLER_DIGEST?=""
HELM_CHART?=""
FORMATTER?=pretty
BATS_CMD_TAGS=BATS_NO_FAIL_FOCUS_RUN=1 BATS_TEST_RETRIES=$(RETRIES) bats \
				--filter-tags $(TAGS) --jobs $(PARALLELISM) $(ARGS) \
				--output /tmp --report-formatter $(FORMATTER)
BATS_CMD=BATS_NO_FAIL_FOCUS_RUN=1 BATS_TEST_RETRIES=$(RETRIES) bats \
		        --jobs $(PARALLELISM) $(ARGS) \
				--output /tmp --report-formatter $(FORMATTER)

test: test-unit test-regression test-k8s ## Run all tests (PARALLELISM=<n>, GPU=[0|1], TAGS=<tags>, RETRIES=<retries>, DEBUG=[0|1])

test-unit: ## Run unit tests (with benchmarks)
	@echo "Running unit tests..."
	$(GOCMD) test -v $(GOMODULE)/...test -bench=. -benchmem

test-regression: ## Run regression tests (PARALLELISM=<n>, GPU=[0|1], TAGS=<tags>, RETRIES=<retries>, DEBUG=[0|1])
	if [ -f /.dockerenv ]; then \
		echo "Running regression tests..." ;\
		echo "Parallelism: $(PARALLELISM)" ;\
		echo "\nUsing unique instance of daemon per test...\n" ;\
		if [ "$(TAGS)" = "" ]; then \
			$(BATS_CMD) -r test/regression ; status_isolated=$$? ;\
		else \
			$(BATS_CMD_TAGS) -r test/regression ; status_isolated=$$? ;\
		fi ;\
		if [ -f /tmp/report.xml ]; then \
			mv /tmp/report.xml /tmp/report-isolated.xml ;\
		fi ;\
		echo "\nUsing a persistent instance of daemon across tests...\n" ;\
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
				RETRIES=$(RETRIES) \
				DEBUG=$(DEBUG) ;\
			$(DOCKER_TEST_REMOVE) ;\
		fi ;\
	fi

test-k8s: ## Run kubernetes e2e tests (PARALLELISM=<n>, GPU=[0|1], TAGS=<tags>, RETRIES=<retries>, DEBUG=[0|1])
	if [ -f /.dockerenv ] || [ "$${PROVIDER,,}" != "k3s" ]; then \
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
			$(DOCKER_TEST_CREATE_NO_PLUGINS_CUDA) ;\
			$(DOCKER_TEST_START) ;\
			$(DOCKER_TEST_EXEC) make test-k8s \
				ARGS=$(ARGS) \
				PARALLELISM=$(PARALLELISM) \
				TAGS=$(TAGS) \
				RETRIES=$(RETRIES) \
				GPU=$(GPU) \
				DEBUG=$(DEBUG) \
				PROVIDER=$(PROVIDER) \
				CLUSTER_ID=$(CLUSTER_ID) \
				SKIP_HELM=$(SKIP_HELM) \
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
			$(DOCKER_TEST_CREATE_NO_PLUGINS) ;\
			$(DOCKER_TEST_START) ;\
			$(DOCKER_TEST_EXEC) make test-k8s \
				ARGS=$(ARGS) \
				PARALLELISM=$(PARALLELISM) \
				TAGS=$(TAGS) \
				RETRIES=$(RETRIES) \
				GPU=$(GPU) \
				DEBUG=$(DEBUG) \
				PROVIDER=$(PROVIDER) \
				CLUSTER_ID=$(CLUSTER_ID) \
				SKIP_HELM=$(SKIP_HELM) \
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
	$(DOCKER_TEST_EXEC) k9s -A -r 1 --logoless --splashless ;\

##########
##@ Docker
##########

# The docker container used as the test environment will have access to the locally installed plugin libraries
# and binaries, *if* they are installed in /usr/local/lib and /usr/local/bin respectively (which is
# the default).

DOCKER_IMAGE=cedana/cedana-helper-test:$(VERSION)
DOCKER_TEST_CONTAINER_NAME=cedana-test
DOCKER_TEST_IMAGE=cedana/cedana-test:latest
DOCKER_TEST_IMAGE_CUDA=cedana/cedana-test:cuda
DOCKER_TEST_START=docker start $(DOCKER_TEST_CONTAINER_NAME) >/dev/null
DOCKER_TEST_EXEC=docker exec -it $(DOCKER_TEST_CONTAINER_NAME)
DOCKER_TEST_REMOVE=docker rm -f $(DOCKER_TEST_CONTAINER_NAME) >/dev/null
PLATFORM=linux/amd64,linux/arm64
ALL_PLUGINS?=1
PREBUILT_BINARIES?=0

PLUGIN_LIB_COPY=find /usr/local/lib -type f -name '*cedana*' -not -name '*gpu*' -exec docker cp {} $(DOCKER_TEST_CONTAINER_NAME):{} \; >/dev/null
PLUGIN_BIN_COPY=find /usr/local/bin -type f -name '*cedana*' -not -name '*gpu*' -exec docker cp {} $(DOCKER_TEST_CONTAINER_NAME):{} \; >/dev/null
PLUGIN_LIB_COPY_GPU=find /usr/local/lib -type f -name '*cedana*' -and -name '*gpu*' -exec docker cp {} $(DOCKER_TEST_CONTAINER_NAME):{} \; >/dev/null
PLUGIN_BIN_COPY_GPU=find /usr/local/bin -type f -name '*cedana*' -and -name '*gpu*' -exec docker cp {} $(DOCKER_TEST_CONTAINER_NAME):{} \; >/dev/null
PLUGIN_BIN_COPY_CRIU=find /usr/local/bin -type f -name 'criu' -exec docker cp {} $(DOCKER_TEST_CONTAINER_NAME):{} \; >/dev/null
HELM_CHART_COPY=if [ -n "$$HELM_CHART" ]; then docker cp $(HELM_CHART) $(DOCKER_TEST_CONTAINER_NAME):/helm-chart ; fi >/dev/null

DOCKER_TEST_CREATE_OPTS=--privileged --init --cgroupns=host --stop-signal=SIGTERM --entrypoint tail --name=$(DOCKER_TEST_CONTAINER_NAME) \
				-v $(PWD):/src:ro -v /var/run/docker.sock:/var/run/docker.sock \
				-e CEDANA_URL=$(CEDANA_URL) -e CEDANA_AUTH_TOKEN=$(CEDANA_AUTH_TOKEN) \
				-e CEDANA_LOG_LEVEL=$(CEDANA_LOG_LEVEL) \
				-e CEDANA_METRICS_ENABLED=$(CEDANA_METRICS_ENABLED) -e CEDANA_PROFILING_ENABLED=$(CEDANA_PROFILING_ENABLED) \
				-e HF_TOKEN=$(HF_TOKEN) \
				-e AWS_ACCESS_KEY_ID=$(AWS_ACCESS_KEY_ID) -e AWS_SECRET_ACCESS_KEY=$(AWS_SECRET_ACCESS_KEY) -e AWS_REGION=$(AWS_REGION) \
				-e GCLOUD_PROJECT_ID=$(GCLOUD_PROJECT_ID) -e GCLOUD_SERVICE_ACCOUNT_KEY='$(GCLOUD_SERVICE_ACCOUNT_KEY)' -e GCLOUD_REGION=$(GCLOUD_REGION) \
				-e EKS_CLUSTER_NAME=$(EKS_CLUSTER_NAME) -e GKE_CLUSTER_NAME=$(GKE_CLUSTER_NAME) -e NB_CLUSTER_NAME=$(NB_CLUSTER_NAME)\
				$(DOCKER_ADDITIONAL_OPTS)
DOCKER_TEST_CREATE=docker create $(DOCKER_TEST_CREATE_OPTS) $(DOCKER_TEST_IMAGE) -f /dev/null >/dev/null && \
						$(PLUGIN_LIB_COPY) && \
						$(PLUGIN_BIN_COPY) && \
						$(PLUGIN_BIN_COPY_CRIU) && \
						$(HELM_CHART_COPY) >/dev/null
DOCKER_TEST_CREATE_NO_PLUGINS=docker create $(DOCKER_TEST_CREATE_OPTS) $(DOCKER_TEST_IMAGE) -f /dev/null >/dev/null && \
						$(HELM_CHART_COPY) >/dev/null
DOCKER_TEST_CREATE_CUDA=docker create --gpus=all --ipc=host $(DOCKER_TEST_CREATE_OPTS) $(DOCKER_TEST_IMAGE_CUDA) -f /dev/null >/dev/null && \
						$(PLUGIN_LIB_COPY) && \
						$(PLUGIN_BIN_COPY) && \
						$(PLUGIN_LIB_COPY_GPU) && \
						$(PLUGIN_BIN_COPY_GPU) && \
						$(PLUGIN_BIN_COPY_CRIU) >/dev/null
DOCKER_TEST_CREATE_NO_PLUGINS_CUDA=docker create --gpus=all --ipc=host $(DOCKER_TEST_CREATE_OPTS) $(DOCKER_TEST_IMAGE_CUDA) -f /dev/null >/dev/null && \
						$(HELM_CHART_COPY) >/dev/null

docker: ## Build the helper Docker image (PLATFORM=linux/amd64,linux/arm64, VERSION=<version>, PREBUILT_BINARIES=[0|1], ALL_PLUGINS=[0|1])
	@echo "Building helper Docker image..."
	docker buildx build --platform $(PLATFORM) \
		--build-arg PREBUILT_BINARIES=$(PREBUILT_BINARIES) \
		--build-arg ALL_PLUGINS=$(ALL_PLUGINS) \
		--build-arg VERSION=$(VERSION) \
		-t $(DOCKER_IMAGE) --load . ;\

docker-push: ## Push the helper Docker image (DOCKER_IMAGE=<image>)
	@echo "Pushing helper Docker image..."
	docker push $(DOCKER_IMAGE)

docker-test: ## Build the test Docker image (PLATFORM=linux/amd64,linux/arm64, DOCKER_TEST_IMAGE=<image>)
	@echo "Building test Docker image..."
	cd test ;\
	docker buildx build --platform $(PLATFORM) -t $(DOCKER_TEST_IMAGE) --load . ;\
	cd -

docker-test-cuda: ## Build the test Docker image for CUDA (PLATFORM=linux/amd64,linux/arm64, DOCKER_TEST_IMAGE_CUDA=<image>)
	@echo "Building test CUDA Docker image..."
	cd test ;\
	docker buildx build --platform $(PLATFORM) -t $(DOCKER_TEST_IMAGE_CUDA) -f cuda.Dockerfile --load . ;\
	cd -

docker-test-push: ## Push the test Docker image (DOCKER_TEST_IMAGE=<image>)
	@echo "Pushing test Docker image..."
	docker push $(DOCKER_TEST_IMAGE)

docker-test-cuda-push: ## Push the test Docker image for CUDA (DOCKER_TEST_IMAGE_CUDA=<image>)
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
