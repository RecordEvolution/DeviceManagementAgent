# IronFlock device management agent (reagent). Run `just` to list recipes.

ROOT_DIR := justfile_directory()
FRP_VERSION := "0.65.0"

# Arch detection for local frpc download (cached). Linux/Darwin/x86_64/aarch64/arm64.
FRP_OS := if os() == "linux" { "linux" } else if os() == "macos" { "darwin" } else { "windows" }
FRP_ARCH := if arch() == "x86_64" { "amd64" } else if arch() == "aarch64" { "arm64" } else if arch() == "arm64" { "arm64" } else { "amd64" }

FRP_CACHE_DIR := ROOT_DIR / ".cache" / "frp"
FRP_CACHED_BINARY := FRP_CACHE_DIR / ("frpc_" + FRP_VERSION + "_" + FRP_OS + "_" + FRP_ARCH)

AGENT_IMAGE_NAME := "europe-docker.pkg.dev/record-1283/eu.gcr.io/ironflock-agent"
AGENT_IMAGE_VERSION := "v" + `cat src/release/version.txt`

default:
    @just --list

# Run unit tests (packages without embedded binary dependency)
test: download-frpc
    cd src && go test -short reagent/messenger reagent/testutil reagent/common reagent/config reagent/debounce reagent/errdefs reagent/safe reagent/apps reagent/api reagent/tunnel

# Run all unit tests (requires frpc binary)
test-all: download-frpc
    cd src && go test -short ./...

# Run unit tests with verbose output
test-verbose:
    cd src && go test -v -short reagent/messenger reagent/testutil

# Run tests with coverage report
test-coverage:
    #!/usr/bin/env bash
    set -euo pipefail
    cd src
    go test -short -coverprofile=coverage.out -covermode=atomic reagent/messenger reagent/testutil reagent/common reagent/config reagent/debounce reagent/errdefs reagent/safe reagent/apps reagent/api reagent/tunnel
    go tool cover -html=coverage.out -o coverage.html
    echo "Coverage report generated at src/coverage.html"

# Run all unit tests with coverage (requires frpc binary)
test-coverage-all: download-frpc
    #!/usr/bin/env bash
    set -euo pipefail
    cd src
    go test -short -coverprofile=coverage.out -covermode=atomic ./...
    go tool cover -html=coverage.out -o coverage.html
    echo "Coverage report generated at src/coverage.html"

# Generate coverage badge
coverage-badge: test-coverage
    #!/usr/bin/env bash
    set -euo pipefail
    cd src
    COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
    if [ $(echo "$COVERAGE >= 80" | bc) -eq 1 ]; then COLOR="brightgreen"
    elif [ $(echo "$COVERAGE >= 60" | bc) -eq 1 ]; then COLOR="green"
    elif [ $(echo "$COVERAGE >= 40" | bc) -eq 1 ]; then COLOR="yellow"
    else COLOR="red"; fi
    echo "Coverage: ${COVERAGE}%"
    curl -s "https://img.shields.io/badge/coverage-${COVERAGE}%25-${COLOR}" > ../assets/coverage-badge.svg
    echo "Badge saved to assets/coverage-badge.svg"

# Run tests with race detector
test-race:
    cd src && go test -short -race reagent/messenger reagent/testutil

# Run messenger package tests only
test-messenger:
    cd src && go test -v reagent/messenger

# Run tunnel package tests only
test-tunnel:
    cd src && go test -v reagent/tunnel

# Download frpc binary for local development (with caching)
download-frpc:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p src/embedded {{FRP_CACHE_DIR}}
    if [ -f "{{FRP_CACHED_BINARY}}" ]; then
        echo "Using cached frpc v{{FRP_VERSION}} for {{FRP_OS}}/{{FRP_ARCH}}"
        cp "{{FRP_CACHED_BINARY}}" src/embedded/frpc_binary
    else
        echo "Downloading frpc v{{FRP_VERSION}} for {{FRP_OS}}/{{FRP_ARCH}}..."
        curl -L "https://github.com/fatedier/frp/releases/download/v{{FRP_VERSION}}/frp_{{FRP_VERSION}}_{{FRP_OS}}_{{FRP_ARCH}}.tar.gz" -o "{{FRP_CACHE_DIR}}/frp.tar.gz"
        tar -xzf "{{FRP_CACHE_DIR}}/frp.tar.gz" -C "{{FRP_CACHE_DIR}}"
        cp "{{FRP_CACHE_DIR}}/frp_{{FRP_VERSION}}_{{FRP_OS}}_{{FRP_ARCH}}/frpc" "{{FRP_CACHED_BINARY}}"
        cp "{{FRP_CACHED_BINARY}}" src/embedded/frpc_binary
        rm -rf "{{FRP_CACHE_DIR}}/frp.tar.gz" "{{FRP_CACHE_DIR}}/frp_{{FRP_VERSION}}_{{FRP_OS}}_{{FRP_ARCH}}"
        echo "Downloaded and cached frpc to {{FRP_CACHED_BINARY}}"
    fi

# Refresh all Go module dependencies. Re-pins the nexus fork
# (RecordEvolution/nexus v4-contrib) to its current branch tip and bumps
# everything else to the latest compatible versions. Go modules pin to a
# pseudo-version, so the nexus fork only moves when this recipe runs.
update-dependencies:
    cd src && go mod edit -replace github.com/gammazero/nexus/v3=github.com/RecordEvolution/nexus/v3@v4-contrib && go get -u ./... && go mod tidy

run:
    cd src && sudo go run -ldflags="-linkmode=external" . -config test-config.flock -prettyLogging -env=local

run_mac: download-frpc
    cd src && sudo DOCKER_HOST=unix://${HOME}/Library/Containers/com.docker.docker/Data/docker.raw.sock go run -ldflags="-linkmode=external" . -config test-config.flock -prettyLogging -env=local -nmw=false

# Builds all docker images for all targets in targets files
build-all-docker: clean
    @mkdir -p {{FRP_CACHE_DIR}}
    docker build --platform linux/amd64 . -t agent-builder
    docker run --name agent_builder -v {{ROOT_DIR}}/build:/app/reagent/build -v {{ROOT_DIR}}/.cache/frp:/app/reagent/.cache/frp agent-builder

# Do everything in one step
rollout: build-all-docker publish release

clean:
    docker rm -f agent_builder
    rm -f build/*
    rm -f src/embedded/frpc_binary

# Publish the new metadata
release: publish-version publish-latestVersions

# Publish the binaries from the build folder
publish:
    scripts/publish.sh

publish-version:
    gsutil cp "src/release/version.txt" gs://re-agent
    gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/version.txt

publish-latestVersions:
    gsutil cp "availableVersions.json" gs://re-agent
    gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/availableVersions.json

# -----------------------------------------------------------------------------
# Official agent docker image (runtime image with prebuilt binary and default
# device config). See docker/Dockerfile.
#
# The image does NOT build the agent itself - it consumes the prebuilt
# binaries produced by `just build-all-docker` from the build/ directory.
# -----------------------------------------------------------------------------

# Build agent runtime image for linux/amd64 (reuses build/reagent-linux-amd64)
build-docker-image-amd64:
    @test -f build/reagent-linux-amd64 || { echo "ERROR: build/reagent-linux-amd64 missing. Run 'just build-all-docker' first."; exit 1; }
    docker build --platform linux/amd64 \
        --build-arg TARGETARCH=amd64 \
        -f docker/Dockerfile \
        -t {{AGENT_IMAGE_NAME}}:{{AGENT_IMAGE_VERSION}}-amd64 \
        -t {{AGENT_IMAGE_NAME}}:latest-amd64 .

# Build agent runtime image for linux/arm64 (reuses build/reagent-linux-arm64)
build-docker-image-arm64:
    @test -f build/reagent-linux-arm64 || { echo "ERROR: build/reagent-linux-arm64 missing. Run 'just build-all-docker' first."; exit 1; }
    docker build --platform linux/arm64 \
        --build-arg TARGETARCH=arm64 \
        -f docker/Dockerfile \
        -t {{AGENT_IMAGE_NAME}}:{{AGENT_IMAGE_VERSION}}-arm64 \
        -t {{AGENT_IMAGE_NAME}}:latest-arm64 .

# Build the official agent runtime image (linux/amd64)
build-docker-image: build-docker-image-amd64

# Iterate locally against the REDeployments stack: cross-compile the
# host-arch linux reagent binary with the host's Go toolchain (much faster
# than `build-all-docker`, which cleans then rebuilds all 8 targets inside
# Docker), package it into the agent runtime image, and tag as the bare
# `:latest` so REDeployments/docker-compose.yml's `agent` service (which
# normally pulls the multi-arch :latest from the registry) picks up the
# local build instead. Recreate the running container with:
#   (cd ../REDeployments && docker compose up -d --force-recreate agent)
# We invoke `docker build` directly (rather than `docker compose build`)
# because the Dockerfile pins the docker-ce apt repo via TARGETARCH and
# compose's hardcoded TARGETARCH=amd64 in the build args mismatches the
# host platform on M1.
build-local:
    scripts/build.sh src build linux/{{FRP_ARCH}}
    @just build-docker-image-{{FRP_ARCH}}
    docker tag {{AGENT_IMAGE_NAME}}:latest-{{FRP_ARCH}} {{AGENT_IMAGE_NAME}}:latest
    @echo "==> Tagged {{AGENT_IMAGE_NAME}}:latest from local linux/{{FRP_ARCH}} build."
    @echo "    Restart with: (cd ../REDeployments && docker compose up -d --force-recreate agent)"

# Push multi-arch agent runtime image manifest
push-docker-image: build-docker-image-amd64 build-docker-image-arm64
    docker push {{AGENT_IMAGE_NAME}}:{{AGENT_IMAGE_VERSION}}-amd64
    docker push {{AGENT_IMAGE_NAME}}:{{AGENT_IMAGE_VERSION}}-arm64
    docker push {{AGENT_IMAGE_NAME}}:latest-amd64
    docker push {{AGENT_IMAGE_NAME}}:latest-arm64
    docker buildx imagetools create -t {{AGENT_IMAGE_NAME}}:{{AGENT_IMAGE_VERSION}} \
        {{AGENT_IMAGE_NAME}}:{{AGENT_IMAGE_VERSION}}-amd64 \
        {{AGENT_IMAGE_NAME}}:{{AGENT_IMAGE_VERSION}}-arm64
    docker buildx imagetools create -t {{AGENT_IMAGE_NAME}}:latest \
        {{AGENT_IMAGE_NAME}}:latest-amd64 \
        {{AGENT_IMAGE_NAME}}:latest-arm64
