#!/bin/bash
version=`cat system/version.txt`

gsutil cp reagent-linux-amd64 "gs://re-agent/linux/amd64/${version}/reagent"
gsutil cp reagent-darwin-10.6-amd64 "gs://re-agent/darwin/amd64/${version}/reagent"
gsutil cp reagent-windows-4.0-amd64.exe "gs://re-agent/windows/amd64/${version}/reagent.exe"

echo "Uploading new version file"
gsutil cp "system/version.txt" gs://re-agent

echo "Flushing cache"
gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent