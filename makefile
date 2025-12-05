.PHONY: build test test-verbose test-coverage test-race

ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
FRP_VERSION:=0.65.0
UNAME_S:=$(shell uname -s)
UNAME_M:=$(shell uname -m)

# Determine frpc architecture based on host system
ifeq ($(UNAME_S),Linux)
	FRP_OS:=linux
else ifeq ($(UNAME_S),Darwin)
	FRP_OS:=darwin
else
	FRP_OS:=windows
endif

ifeq ($(UNAME_M),x86_64)
	FRP_ARCH:=amd64
else ifeq ($(UNAME_M),aarch64)
	FRP_ARCH:=arm64
else ifeq ($(UNAME_M),arm64)
	FRP_ARCH:=arm64
else
	FRP_ARCH:=amd64
endif

help: ## This help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help

test: ## Run unit tests (packages without embedded binary dependency)
	cd src && go test -short reagent/messenger reagent/testutil reagent/common reagent/config reagent/debounce reagent/errdefs reagent/safe

test-all: download-frpc ## Run all unit tests excluding tunnel (requires frpc binary)
	cd src && go test -short $$(go list ./... | grep -v reagent/tunnel)

test-verbose: ## Run unit tests with verbose output
	cd src && go test -v -short reagent/messenger reagent/testutil

test-coverage: ## Run tests with coverage report
	cd src && go test -short -coverprofile=coverage.out -covermode=atomic reagent/messenger reagent/testutil reagent/common reagent/config reagent/debounce reagent/errdefs reagent/safe
	cd src && go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated at src/coverage.html"

test-coverage-all: download-frpc ## Run all tests with coverage excluding tunnel (requires frpc binary)
	cd src && go test -short -coverprofile=coverage.out -covermode=atomic $$(go list ./... | grep -v reagent/tunnel)
	cd src && go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated at src/coverage.html"

coverage-badge: test-coverage ## Generate coverage badge
	@cd src && COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//') && \
	if [ $$(echo "$$COVERAGE >= 80" | bc) -eq 1 ]; then COLOR="brightgreen"; \
	elif [ $$(echo "$$COVERAGE >= 60" | bc) -eq 1 ]; then COLOR="green"; \
	elif [ $$(echo "$$COVERAGE >= 40" | bc) -eq 1 ]; then COLOR="yellow"; \
	else COLOR="red"; fi && \
	echo "Coverage: $${COVERAGE}%" && \
	curl -s "https://img.shields.io/badge/coverage-$${COVERAGE}%25-$${COLOR}" > ../assets/coverage-badge.svg && \
	echo "Badge saved to assets/coverage-badge.svg"

test-race: ## Run tests with race detector
	cd src && go test -short -race reagent/messenger reagent/testutil

test-messenger: ## Run messenger package tests only
	cd src && go test -v reagent/messenger

test-tunnel: ## Run tunnel package tests only
	cd src && go test -v reagent/tunnel

FRP_CACHE_DIR:=$(ROOT_DIR)/.cache/frp
FRP_CACHED_BINARY:=$(FRP_CACHE_DIR)/frpc_$(FRP_VERSION)_$(FRP_OS)_$(FRP_ARCH)

download-frpc: ## Download frpc binary for local development (with caching)
	@mkdir -p src/embedded $(FRP_CACHE_DIR)
	@if [ -f "$(FRP_CACHED_BINARY)" ]; then \
		echo "Using cached frpc v$(FRP_VERSION) for $(FRP_OS)/$(FRP_ARCH)"; \
		cp "$(FRP_CACHED_BINARY)" src/embedded/frpc_binary; \
	else \
		echo "Downloading frpc v$(FRP_VERSION) for $(FRP_OS)/$(FRP_ARCH)..."; \
		curl -L "https://github.com/fatedier/frp/releases/download/v$(FRP_VERSION)/frp_$(FRP_VERSION)_$(FRP_OS)_$(FRP_ARCH).tar.gz" -o "$(FRP_CACHE_DIR)/frp.tar.gz"; \
		tar -xzf "$(FRP_CACHE_DIR)/frp.tar.gz" -C "$(FRP_CACHE_DIR)"; \
		cp "$(FRP_CACHE_DIR)/frp_$(FRP_VERSION)_$(FRP_OS)_$(FRP_ARCH)/frpc" "$(FRP_CACHED_BINARY)"; \
		cp "$(FRP_CACHED_BINARY)" src/embedded/frpc_binary; \
		rm -rf "$(FRP_CACHE_DIR)/frp.tar.gz" "$(FRP_CACHE_DIR)/frp_$(FRP_VERSION)_$(FRP_OS)_$(FRP_ARCH)"; \
		echo "Downloaded and cached frpc to $(FRP_CACHED_BINARY)"; \
	fi

run:
	cd src && sudo go run -ldflags="-linkmode=external" . -config test-config.flock -prettyLogging -env=local

run_mac: donwload-frpc
	cd src && sudo DOCKER_HOST=unix://${HOME}/Library/Containers/com.docker.docker/Data/docker.raw.sock go run -ldflags="-linkmode=external" . -config test-config.flock -prettyLogging -env=local -nmw=false

# Not preferred. Use build-all-docker instead.
build-all:
	scripts/build-all.sh

build-all-docker: clean ## Builds all docker images for all targets in targets files
	@mkdir -p $(FRP_CACHE_DIR)
	docker build --platform linux/amd64 . -t agent-builder
	docker run --name agent_builder -v ${ROOT_DIR}/build:/app/reagent/build -v ${ROOT_DIR}/.cache/frp:/app/reagent/.cache/frp agent-builder

rollout: build-all-docker publish release ## Do everythin in one step

clean:
	docker rm -f agent_builder
	rm -f build/*
	rm -f src/embedded/frpc_binary

release: publish-version publish-latestVersions ## publish the new metadata

publish: # publish the binaries from the build folder
	scripts/publish.sh

publish-version:
	gsutil cp "src/release/version.txt" gs://re-agent
	gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/version.txt

publish-latestVersions:
	gsutil cp "availableVersions.json" gs://re-agent
	gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/availableVersions.json