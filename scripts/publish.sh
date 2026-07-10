#!/bin/bash

array=($(ls build))
VERSION=`cat src/release/version.txt`

# Portable SHA-256 (ubuntu CI has sha256sum, macOS has shasum).
sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

for element in "${array[@]}"; do
    case "$element" in *.sha256) continue ;; esac

    OS=$(echo "$element" | cut -d "-" -f 2)
    ARCH=$(echo "$element" | cut -d "-" -f 3)
    BINARY_NAME="reagent"

    if [ "$OS" == "windows" ]; then
        BINARY_NAME="reagent.exe"
    fi

    GCLOUD="gs://re-agent/${OS}/${ARCH}/${VERSION}/${BINARY_NAME}"
    gsutil cp "build/$element" $GCLOUD

    # SHA-256 manifest published next to each binary: the agent verifies OTA
    # downloads against it (system.verifyRemoteChecksum) and the Windows
    # service refuses to activate an update that does not match.
    sha256_of "build/$element" > "build/$element.sha256"
    gsutil cp "build/$element.sha256" "${GCLOUD}.sha256"
    rm -f "build/$element.sha256"
done

gsutil -m setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent
