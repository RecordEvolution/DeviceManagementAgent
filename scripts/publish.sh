#!/bin/bash
# Publish the built agent binaries (build/reagent-<os>-<arch>) to
# gs://re-agent/<os>/<arch>/<version>/reagent[.exe], each with two sidecars:
#
#   .sha256  SHA-256 manifest: the agent verifies OTA downloads against it
#            (system.verifyRemoteChecksum) and the Windows service refuses to
#            activate an update that does not match.
#   .sig     Ed25519 update signature, written by `cmd/sign-update sign`
#            (release.yml, or `just sign-binaries`): agents refuse an update
#            without a valid one for exactly <os>/<arch>/<version>/reagent[.exe]
#            (system.verifyRemoteSignature).
#
# Nothing is uploaded unless every binary carries a signature that verifies,
# against this build's AGENT_UPDATE_PUBLIC_KEYS, for the path it goes to.
set -euo pipefail

VERSION=$(cat src/release/version.txt)

# Portable SHA-256 (ubuntu CI has sha256sum, macOS has shasum).
sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

# Agent binaries only: build/ also holds frpc-windows-amd64 in release CI
# (publish-frpc.sh publishes that), and sidecars or SBOMs locally.
binaries=()
for path in build/reagent-*; do
    name=$(basename "$path")
    [[ "$name" =~ ^reagent-[a-z]+-[a-z0-9]+$ ]] || continue
    binaries+=("$name")
done
if [ "${#binaries[@]}" -eq 0 ]; then
    echo "no agent binaries (build/reagent-<os>-<arch>) to publish" >&2
    exit 1
fi

unsigned=0
for element in "${binaries[@]}"; do
    if [ ! -s "build/$element.sig" ]; then
        echo "build/$element.sig is missing" >&2
        unsigned=1
    fi
done
if [ "$unsigned" -ne 0 ]; then
    echo "sign the binaries first: just sign-binaries (needs AGENT_UPDATE_SIGNING_KEY)" >&2
    exit 1
fi

# Same <os>/<arch> derivation from the build name as below, same VERSION: a
# signature made for another version (version.txt bumped after signing) fails.
(cd src && go run ./cmd/sign-update verify -version "$VERSION" "${binaries[@]/#/../build/}")

for element in "${binaries[@]}"; do
    OS=$(echo "$element" | cut -d "-" -f 2)
    ARCH=$(echo "$element" | cut -d "-" -f 3)
    BINARY_NAME="reagent"

    if [ "$OS" == "windows" ]; then
        BINARY_NAME="reagent.exe"
    fi

    GCLOUD="gs://re-agent/${OS}/${ARCH}/${VERSION}/${BINARY_NAME}"
    gsutil cp "build/$element" "$GCLOUD"

    sha256_of "build/$element" > "build/$element.sha256"
    gsutil cp "build/$element.sha256" "${GCLOUD}.sha256"
    rm -f "build/$element.sha256"

    gsutil cp "build/$element.sig" "${GCLOUD}.sig"
done

gsutil -m setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent
