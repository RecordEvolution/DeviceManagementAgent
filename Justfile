# IronFlock device management agent (reagent). Run `just` to list recipes.

ROOT_DIR := justfile_directory()
FRP_VERSION := "0.69.1"
MOCKERY_VERSION := "v3.7.1"
GOVULNCHECK_VERSION := "v1.3.0"
CYCLONEDX_GOMOD_VERSION := "v1.10.0"

# Arch detection for local frpc download (cached). Linux/Darwin/x86_64/aarch64/arm64.
FRP_OS := if os() == "linux" { "linux" } else if os() == "macos" { "darwin" } else { "windows" }
FRP_ARCH := if arch() == "x86_64" { "amd64" } else if arch() == "aarch64" { "arm64" } else if arch() == "arm64" { "arm64" } else { "amd64" }

FRP_CACHE_DIR := ROOT_DIR / ".cache" / "frp"
FRP_CACHED_BINARY := FRP_CACHE_DIR / ("frpc_" + FRP_VERSION + "_" + FRP_OS + "_" + FRP_ARCH)

AGENT_IMAGE_NAME := "europe-docker.pkg.dev/record-1283/eu.gcr.io/ironflock-agent"
AGENT_IMAGE_VERSION := "v" + `cat src/release/version.txt`

default:
    @just --list

# Run unit tests (integration-tagged tests excluded; needs frpc — see src/TESTING.md).
test: download-frpc
    cd src && go test -short ./...

# Run integration tests (need external resources: frps server, docker daemon, dbus).
test-integration: download-frpc
    cd src && go test -tags integration ./...

# Run unit tests with an HTML coverage report at src/coverage.html.
test-coverage: download-frpc
    #!/usr/bin/env bash
    set -euo pipefail
    cd src
    go test -short -coverprofile=coverage.out -covermode=atomic ./...
    go tool cover -html=coverage.out -o coverage.html
    echo "Coverage report generated at src/coverage.html"

# Integration tests self-skip when their resource (docker/dbus/frps) is absent,
# so this is safe to run anywhere; it just covers more where they exist.
# Coverage INCLUDING integration-tagged tests.
test-coverage-integration: download-frpc
    #!/usr/bin/env bash
    set -euo pipefail
    cd src
    go test -tags integration -coverprofile=coverage.out -covermode=atomic ./...
    go tool cover -html=coverage.out -o coverage.html
    echo "Coverage (incl. integration) report generated at src/coverage.html"

# Excludes generated D-Bus bindings (networkmanager), generated test doubles
# (testutil), and tooling (embedded, benchmark) from the denominator, and
# includes integration tests — you do not unit-test generated code or your mocks.
# Production-scoped coverage (the meaningful number).
test-coverage-prod: download-frpc
    #!/usr/bin/env bash
    set -euo pipefail
    cd src
    PKGS=$(go list ./... | grep -vE '/(testutil|networkmanager|embedded|benchmark)(/|$)')
    COVERPKG=$(echo "$PKGS" | paste -sd, -)
    go test -tags integration -covermode=atomic -coverpkg="$COVERPKG" -coverprofile=coverage.out $PKGS
    echo "----- production-scoped coverage (generated + test-infra excluded) -----"
    go tool cover -func=coverage.out | grep total

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

# Run unit tests with the race detector.
test-race: download-frpc
    cd src && go test -short -race ./...

# go vet excludes the generated networkmanager bindings (benign codegen vet note).
# Static checks: gofmt, go mod tidy, go vet, and a full compile.
lint: download-frpc
    #!/usr/bin/env bash
    set -euo pipefail
    cd src
    echo "==> gofmt"
    unformatted=$(gofmt -l .)
    if [ -n "$unformatted" ]; then echo "unformatted (run: gofmt -w src):"; echo "$unformatted"; exit 1; fi
    echo "==> go mod tidy (diff)"
    go mod tidy -diff
    echo "==> go vet"
    go vet -tags integration $(go list ./... | grep -v '/networkmanager')
    echo "==> go build"
    go build ./...
    echo "==> lint OK"

# Runs EVERYTHING: lint, then unit tests under the race detector + randomized
# order, then the full integration suite with coverage. (Integration runs WITHOUT
# -race — docker/PTY + race is timing-flaky; tests self-skip absent resources.)
# The slowest, most thorough local check — what CI must pass to release.
test-all: lint
    #!/usr/bin/env bash
    set -euo pipefail
    cd src
    echo "==> unit tests (race + shuffle, no cache)"
    go test -race -shuffle=on -count=1 ./...
    echo "==> integration suite + coverage (no race; self-skips absent resources)"
    go test -tags integration -covermode=atomic -coverprofile=coverage.out -count=1 ./...
    echo "==> coverage:"
    go tool cover -func=coverage.out | tail -1
    echo "==> ALL checks passed"

# Regenerate testify mocks into src/testutil/mocks from .mockery.yaml.
test-generate-mocks:
    cd src && go run github.com/vektra/mockery/v3@{{MOCKERY_VERSION}}

# -----------------------------------------------------------------------------
# Security / CVE screening (report-only). Scans the production artifact: our Go
# code + all module dependencies, plus the embedded third-party frpc binary.
# The Docker images and the edge host OS are out of scope (the agent ships as a
# bare binary that runs on the device host). See docs/SECURITY-SCANNING.md.
# -----------------------------------------------------------------------------

# Reachability-aware; honours the nexus `replace`. Exit 3 = reachable vulns found.
# Scan our Go code + all module dependencies for CVEs.
vuln-go:
    cd src && go run golang.org/x/vuln/cmd/govulncheck@{{GOVULNCHECK_VERSION}} ./...

# Flags CVEs in frp itself + its bundled deps; remediate by bumping FRP_VERSION.
# Scan the embedded third-party frpc binary (not tracked in go.mod) for CVEs.
vuln-frpc: download-frpc
    go run golang.org/x/vuln/cmd/govulncheck@{{GOVULNCHECK_VERSION}} -mode=binary src/embedded/frpc_binary

# Generate CycloneDX SBOMs (Go module graph + the frpc binary) into build/sbom/.
sbom: download-frpc
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p build/sbom
    VERSION=$(cat src/release/version.txt)
    (cd src && go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@{{CYCLONEDX_GOMOD_VERSION}} mod -licenses -json -output "../build/sbom/reagent-${VERSION}.cdx.json" .)
    go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@{{CYCLONEDX_GOMOD_VERSION}} bin -json -output "build/sbom/frpc-{{FRP_VERSION}}.cdx.json" src/embedded/frpc_binary
    echo "SBOMs written to build/sbom/"

# Used by the CI SBOM-attestation workflow; bin mode reads the actual build info.
# Generate a CycloneDX SBOM for ONE compiled binary (writes <binary>.cdx.json).
sbom-bin BINARY:
    go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@{{CYCLONEDX_GOMOD_VERSION}} bin -json -output "{{BINARY}}.cdx.json" "{{BINARY}}"

# Uploaded to the GitHub Security tab by CI; SARIF exits 0 even with findings.
# Generate govulncheck SARIF reports into build/sarif/ for GitHub code scanning.
sarif: download-frpc
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p build/sarif
    (cd src && go run golang.org/x/vuln/cmd/govulncheck@{{GOVULNCHECK_VERSION}} -format sarif ./... > ../build/sarif/govulncheck-code.sarif)
    go run golang.org/x/vuln/cmd/govulncheck@{{GOVULNCHECK_VERSION}} -format sarif -mode=binary src/embedded/frpc_binary > build/sarif/govulncheck-frpc.sarif
    # Post-process so GitHub Code Scanning accepts the code SARIF:
    #  - dedup result.stacks (rejected as "contains duplicate item");
    #  - prefix file URIs with src/ — govulncheck runs inside src/, so its paths
    #    (e.g. go.mod) are src-relative and must resolve at the repo root.
    # The frpc binary SARIF has NO source locations (it scans a compiled binary),
    # so it cannot go to Code Scanning — security.yml uploads only the code SARIF;
    # frpc findings remain in the job's log summary + the SARIF artifact.
    for f in build/sarif/govulncheck-code.sarif build/sarif/govulncheck-frpc.sarif; do
        jq '.runs |= map(.results |= map(if .stacks then .stacks |= unique else . end))' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    done
    jq 'walk(if (type=="object" and (.uri? | type=="string") and (.uri|test("^(src/|/|[a-z]+://)")|not)) then .uri |= "src/" + . else . end)' build/sarif/govulncheck-code.sarif > build/sarif/govulncheck-code.sarif.tmp
    mv build/sarif/govulncheck-code.sarif.tmp build/sarif/govulncheck-code.sarif
    echo "SARIF written to build/sarif/"

# Scans are inlined (not `just vuln-go`) so a "vulns found" non-zero exit doesn't
# print a confusing "Recipe failed" line; the pinned version stays single-sourced.
# Full CVE screening (report-only — never fails). Runs all scans + SBOMs.
security: download-frpc
    #!/usr/bin/env bash
    set +e
    echo "==> govulncheck: Go code + dependencies"
    (cd src && go run golang.org/x/vuln/cmd/govulncheck@{{GOVULNCHECK_VERSION}} ./...)
    echo "==> govulncheck: embedded frpc binary"
    go run golang.org/x/vuln/cmd/govulncheck@{{GOVULNCHECK_VERSION}} -mode=binary src/embedded/frpc_binary
    echo "==> SBOM generation"
    just sbom
    echo "==> security screening complete (report-only)"
    exit 0

# Needs a prior `just build-all-docker`; validates the exact shipped artifacts.
# Optional: CVE-scan every cross-compiled binary in build/ (heavier).
vuln-binaries:
    #!/usr/bin/env bash
    set +e
    for bin in build/reagent-*; do
        echo "==> $bin"
        go run golang.org/x/vuln/cmd/govulncheck@{{GOVULNCHECK_VERSION}} -mode=binary "$bin"
    done

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

# Follow with `just release` to tag + trigger the build/publish CI.
# Bump the patch version in src/release/version.txt and commit it.
bump-patch:
    #!/usr/bin/env bash
    set -euo pipefail
    cd {{ROOT_DIR}}
    current=$(tr -d '[:space:]' < src/release/version.txt)
    if [[ ! "$current" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "src/release/version.txt is not MAJOR.MINOR.PATCH: '$current'" >&2
        exit 1
    fi
    IFS=. read -r major minor patch <<< "$current"
    next="${major}.${minor}.$((patch + 1))"
    printf '%s' "$next" > src/release/version.txt
    jq --arg v "$next" 'map_values($v)' availableVersions.json > availableVersions.json.tmp && mv availableVersions.json.tmp availableVersions.json
    echo "Bumped $current -> $next (all channels in availableVersions.json set to $next). Now commit everything and run: just release"

# Follow with `just release` to tag + trigger the build/publish CI.
# Bump the minor version in src/release/version.txt (resets patch) and commit it.
bump-minor:
    #!/usr/bin/env bash
    set -euo pipefail
    cd {{ROOT_DIR}}
    current=$(tr -d '[:space:]' < src/release/version.txt)
    if [[ ! "$current" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "src/release/version.txt is not MAJOR.MINOR.PATCH: '$current'" >&2
        exit 1
    fi
    IFS=. read -r major minor patch <<< "$current"
    next="${major}.$((minor + 1)).0"
    printf '%s' "$next" > src/release/version.txt
    jq --arg v "$next" 'map_values($v)' availableVersions.json > availableVersions.json.tmp && mv availableVersions.json.tmp availableVersions.json
    echo "Bumped $current -> $next (all channels in availableVersions.json set to $next). Now commit everything and run: just release"

# Follow with `just release` to tag + trigger the build/publish CI.
# Bump the major version in src/release/version.txt (resets minor and patch) and commit it.
bump-major:
    #!/usr/bin/env bash
    set -euo pipefail
    cd {{ROOT_DIR}}
    current=$(tr -d '[:space:]' < src/release/version.txt)
    if [[ ! "$current" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "src/release/version.txt is not MAJOR.MINOR.PATCH: '$current'" >&2
        exit 1
    fi
    IFS=. read -r major minor patch <<< "$current"
    next="$((major + 1)).0.0"
    printf '%s' "$next" > src/release/version.txt
    jq --arg v "$next" 'map_values($v)' availableVersions.json > availableVersions.json.tmp && mv availableVersions.json.tmp availableVersions.json
    echo "Bumped $current -> $next (all channels in availableVersions.json set to $next). Now commit everything and run: just release"

# Requires a clean working tree; promote afterwards with `just promote`.
# Tag the current commit as v<version.txt> and push (triggers build/publish CI).
# Runs the lint gate locally FIRST: release.yml requires gate.yml to pass before
# publishing, but the tag is already pushed by then — a lint failure would leave a
# broken v<version> tag on origin to clean up. Catch it here, before the push.
release:
    #!/usr/bin/env bash
    set -euo pipefail
    cd {{ROOT_DIR}}
    if [[ -n "$(git status --porcelain)" ]]; then
        echo "working tree is not clean — commit everything first (e.g. just bump-patch)" >&2
        exit 1
    fi
    version=$(tr -d '[:space:]' < src/release/version.txt)
    if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "src/release/version.txt is not MAJOR.MINOR.PATCH: '$version'" >&2
        exit 1
    fi
    echo "==> running lint gate locally before pushing the release tag..."
    {{just_executable()}} lint
    scripts/check-frp-version.sh
    git tag -a "v${version}" -m "release v${version}"
    git push origin "$(git rev-parse --abbrev-ref HEAD)"
    git push origin "v${version}"
    echo "Pushed v${version}; the release workflow will build + publish. Adjust availableVersions.json and promote with: just promote"

# Do everything in one step
rollout: build-all-docker publish promote

clean:
    docker rm -f agent_builder
    rm -f build/*
    rm -f src/embedded/frpc_binary

# Promote a staged version to live: publish version.txt + availableVersions.json.
# This is the manual release gate — agents read availableVersions.json.
promote: publish-version publish-latestVersions

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
