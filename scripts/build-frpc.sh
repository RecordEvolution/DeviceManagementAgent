#!/usr/bin/env bash
# Build frpc from source at the pinned FRP_VERSION for a target platform.
#
# We build frpc ourselves rather than shipping fatedier/frp release binaries so
# that (a) we control the supply chain, (b) we can Authenticode-sign the result,
# and (c) the hash differs from the widely-flagged official binary. Signing does
# NOT clear frp's intentional PUA/HackTool antivirus classification — that is
# handled operationally on-device (Defender exclusion / WDAC signer rule) — but
# a self-built, signed binary is still the right supply-chain posture.
#
# Usage: scripts/build-frpc.sh <goos> <goarch> <output-path>
set -euo pipefail

target_os="${1:?usage: build-frpc.sh <goos> <goarch> <output-path>}"
target_arch="${2:?usage: build-frpc.sh <goos> <goarch> <output-path>}"
out="${3:?usage: build-frpc.sh <goos> <goarch> <output-path>}"

FRP_VERSION="$(grep -oE 'FRP_VERSION = "[^"]+"' src/embedded/frpc.go | cut -d'"' -f2)"
echo "Building frpc v${FRP_VERSION} for ${target_os}/${target_arch}"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

# Exact upstream tag, shallow clone; build with upstream's own go.mod (no
# re-pinned deps, no custom flags) so the client stays wire-compatible with an
# frps server built from the same tag.
git clone --depth 1 --branch "v${FRP_VERSION}" https://github.com/fatedier/frp "$workdir/frp"

out_abs="$(cd "$(dirname "$out")" && pwd)/$(basename "$out")"
(
    cd "$workdir/frp"
    # -tags noweb skips the embedded web dashboard (its dist assets aren't in
    # the git tree — they need a separate frontend build). The agent drives
    # frpc through its admin HTTP API, not the dashboard, so noweb is complete
    # for our use and keeps the build self-contained (no Node toolchain).
    GOOS="$target_os" GOARCH="$target_arch" CGO_ENABLED=0 go build -trimpath -tags noweb -o "$out_abs" ./cmd/frpc
)

echo "Built frpc -> $out_abs ($(wc -c < "$out_abs") bytes)"
