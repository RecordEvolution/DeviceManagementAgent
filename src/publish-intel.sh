#!/bin/bash

rm reagent-darwin-10.6-amd64 || true
rm reagent-linux-amd64 || true
rm reagent-windows-4.0-amd64.exe || true
echo "Building Intel Binaries for all Windows and Linux systems"
${GOPATH}/bin/xgo -v --targets=*/amd64 .

gsutil cp reagent-linux-amd64 gs://re-agent
gsutil cp reagent-darwin-10.6-amd64 gs://re-agent
gsutil cp reagent-windows-4.0-amd64.exe gs://re-agent

echo "Flushing cache"
gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent