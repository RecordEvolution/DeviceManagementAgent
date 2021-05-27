#!/bin/bash

rm reagent-darwin-10.6-amd64 || true
rm reagent-linux-amd64 || true
rm reagent-windows-4.0-amd64.exe || true
echo "Building Intel Binaries for all Windows and Linux systems"

${GOPATH}/bin/xgo -v --targets=*/amd64 .