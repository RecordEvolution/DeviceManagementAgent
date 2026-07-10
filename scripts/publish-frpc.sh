#!/usr/bin/env bash
# Publish a (already built and, in release CI, signed) frpc binary to the
# re-agent bucket under a /frpc/ sub-path, with a SHA-256 manifest next to it.
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

dest="gs://re-agent/frpc/${goos}/${goarch}/${FRP_VERSION}/${remote_name}"
gsutil cp "$binary" "$dest"

sha256_of "$binary" > "${binary}.sha256"
gsutil cp "${binary}.sha256" "${dest}.sha256"
rm -f "${binary}.sha256"

gsutil -m setmeta -r -h "Cache-control:public, max-age=0" "gs://re-agent/frpc"

echo "Published $dest (+ .sha256)"
