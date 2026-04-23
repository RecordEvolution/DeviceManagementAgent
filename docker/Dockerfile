# syntax=docker/dockerfile:1
#
# Official IronFlock Device Management Agent runtime image.
#
# This Dockerfile does NOT build the agent. It expects a prebuilt reagent
# binary (produced by `make build-all-docker`) to be available under
# ./build/reagent-linux-<arch> at the repo root.
#
# Build context: the repository root (DeviceManagementAgent).
#
# Examples:
#   # build linux/amd64 image using the existing prebuilt binary
#   make build-all-docker                                  # produces build/reagent-linux-amd64 etc.
#   docker build -f docker/Dockerfile \
#       --build-arg TARGETARCH=amd64 \
#       -t ironflock-agent:local .

FROM ubuntu:22.04

ARG TARGETARCH=amd64
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
        apt-transport-https \
        ca-certificates \
        curl \
        gnupg \
        lsb-release \
        dbus \
        network-manager \
        jq \
        libc6 \
        musl \
    && curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
        | gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg \
    && echo \
        "deb [arch=${TARGETARCH} signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" \
        > /etc/apt/sources.list.d/docker.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends docker-ce-cli \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /opt/reagent /app

WORKDIR /app

# Copy the prebuilt binary for the requested arch. Run
# `make build-all-docker` (or `make build-all`) first to produce it.
COPY build/reagent-linux-${TARGETARCH} /usr/local/bin/reagent
RUN chmod +x /usr/local/bin/reagent

COPY docker/entrypoint.sh /app/entrypoint.sh

RUN chmod +x /app/entrypoint.sh

# Default environment (overridable by docker-compose).
ENV AGENT_ENV=local \
    AGENT_NMW=false \
    AGENT_CONFIG_PATH=/app/device-config.flock

ENTRYPOINT ["/app/entrypoint.sh"]
