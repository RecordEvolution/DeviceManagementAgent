#!/bin/bash

rm reagent-linux-arm64 || true
echo "Building Intel Binaries for all Windows and Linux systems"

${HOME}/go/bin/xgo -v -ldflags "-X 'reagent/system.BuildArch=arm64'" --targets=linux/arm64 .