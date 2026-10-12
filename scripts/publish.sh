#!/bin/bash
# Stage the built agent binaries (build/reagent-<os>-<arch>) at
# gs://re-agent/<os>/<arch>/<version>/reagent[.exe], each with its .sha256
# manifest: the agent verifies OTA downloads against it
# (system.verifyRemoteChecksum) and the Windows service refuses to activate an
# update that does not match.
#
# Staged binaries carry no .sig: agents refuse them until `just promote` signs
# them through OpenBao (scripts/sign-release.sh). A local build/<binary>.sig
# (a development signature) is uploaded too, but only if it verifies, against
# this tree's AGENT_UPDATE_PUBLIC_KEYS, for the path it goes to.
#
# A version that is already signed in the bucket has been promoted; restaging
# it would put new bytes under its live .sig, so that is refused.
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

remote_path() {
    local os arch binary_name="reagent"
    os=$(echo "$1" | cut -d "-" -f 2)
    arch=$(echo "$1" | cut -d "-" -f 3)
    if [ "$os" == "windows" ]; then
        binary_name="reagent.exe"
    fi
    echo "gs://re-agent/${os}/${arch}/${VERSION}/${binary_name}"
}

signed=()
for element in "${binaries[@]}"; do
    if [ -e "build/$element.sig" ]; then
        signed+=("../build/$element")
    elif gsutil -q stat "$(remote_path "$element").sig"; then
        echo "$(remote_path "$element") is already signed (promoted); bump the version instead of restaging it" >&2
        exit 1
    fi
done

# Same <os>/<arch> derivation from the build name as remote_path, same
# VERSION: a signature made for another version or platform fails.
if [ "${#signed[@]}" -gt 0 ]; then
    (cd src && go run ./cmd/sign-update verify -version "$VERSION" "${signed[@]}")
fi

for element in "${binaries[@]}"; do
    GCLOUD=$(remote_path "$element")
    gsutil cp "build/$element" "$GCLOUD"

    sha256_of "build/$element" > "build/$element.sha256"
    gsutil cp "build/$element.sha256" "${GCLOUD}.sha256"
    rm -f "build/$element.sha256"

    if [ -e "build/$element.sig" ]; then
        gsutil cp "build/$element.sig" "${GCLOUD}.sig"
    fi
done

gsutil -m setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent
