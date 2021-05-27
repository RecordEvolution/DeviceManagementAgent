#!/bin/bash

rm reagent-linux-arm64 || true
echo "Building Intel Binaries for all Windows and Linux systems"

${GOPATH}/bin/xgo -v --targets=linux/arm64 .