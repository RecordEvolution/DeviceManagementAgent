#!/usr/bin/env bash
# Publish a (already built and, in release CI, Authenticode-signed) frpc binary
# to the re-agent bucket under a /frpc/ sub-path, with a SHA-256 manifest and
# its Ed25519 signature (<binary>.sig from `just sign-binaries`) next to it.
# The agent refuses an frpc without a valid frpc-download signature for exactly
# <goos>/<goarch>/<FRP_VERSION>/frpc[.exe] (system.verifyRemoteSignature), so
# nothing is uploaded unless the .sig verifies for that path.
#
# It rides the EXISTING re-agent bucket + appliance /dl/re-agent/* proxy route
# (the proxy forwards everything after the first path segment verbatim), so no
# server-side route change is needed. The agent downloads it via
# downloadBinary("frpc", "re-agent/frpc", FRP_VERSION, ...).
#
# Usage: scripts/publish-frpc.sh <goos> <goarch> <frpc-binary-path>
set -euo pipefail

goos="${1:?usage: publish-frpc.sh <goos> <goarch> <path>}"
goarch="${2:?usage: publish-frpc.sh <goos> <goarch> <path>}"
binary="${3:?usage: publish-frpc.sh <goos> <goarch> <path>}"

FRP_VERSION="$(grep -oE 'FRP_VERSION = "[^"]+"' src/embedded/frpc.go | cut -d'"' -f2)"

remote_name="frpc"
if [ "$goos" = "windows" ]; then
    remote_name="frpc.exe"
fi

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

if [ ! -s "${binary}.sig" ]; then
    echo "${binary}.sig is missing: sign it first (just sign-binaries, needs AGENT_UPDATE_SIGNING_KEY)" >&2
    exit 1
fi
binary_abs="$(cd "$(dirname "$binary")" && pwd)/$(basename "$binary")"
(cd src && go run ./cmd/sign-update verify -kind frpc -os "$goos" -arch "$goarch" -version "$FRP_VERSION" "$binary_abs")

dest="gs://re-agent/frpc/${goos}/${goarch}/${FRP_VERSION}/${remote_name}"
gsutil cp "$binary" "$dest"

sha256_of "$binary" > "${binary}.sha256"
gsutil cp "${binary}.sha256" "${dest}.sha256"
rm -f "${binary}.sha256"

gsutil cp "${binary}.sig" "${dest}.sig"

gsutil -m setmeta -r -h "Cache-control:public, max-age=0" "gs://re-agent/frpc"

echo "Published $dest (+ .sha256, .sig)"
