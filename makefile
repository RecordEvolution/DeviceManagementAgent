.PHONY: build

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

download-frpc: ## Download frpc binary for local development
	@echo "Downloading frpc v$(FRP_VERSION) for $(FRP_OS)/$(FRP_ARCH)..."
	@mkdir -p src/embedded
	@curl -L "https://github.com/fatedier/frp/releases/download/v$(FRP_VERSION)/frp_$(FRP_VERSION)_$(FRP_OS)_$(FRP_ARCH).tar.gz" -o /tmp/frp.tar.gz
	@tar -xzf /tmp/frp.tar.gz -C /tmp
	@cp /tmp/frp_$(FRP_VERSION)_$(FRP_OS)_$(FRP_ARCH)/frpc src/embedded/frpc_binary
	@rm -rf /tmp/frp.tar.gz /tmp/frp_$(FRP_VERSION)_$(FRP_OS)_$(FRP_ARCH)
	@echo "Downloaded frpc to src/embedded/frpc_binary"

run:
	cd src && sudo go run . -config test-config.flock -prettyLogging -env=local -prettyLogging

run_mac:
	cd src && DOCKER_HOST=unix://${HOME}/Library/Containers/com.docker.docker/Data/docker.raw.sock go run . -config test-config.flock -prettyLogging -env=local -nmw=false

# Not preferred. Use build-all-docker instead.
build-all:
	scripts/build-all.sh

build-all-docker: clean ## Builds all docker images for all targets in targets files
	docker build --platform linux/amd64 . -t agent-builder
	docker run --name agent_builder -v ${ROOT_DIR}/build:/app/reagent/build agent-builder

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