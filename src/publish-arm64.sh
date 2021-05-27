#!/bin/bash
version=`cat system/version.txt`

echo "Uploading New Agent Binary"
gsutil cp reagent-linux-arm64 "gs://re-agent/linux/arm64/${version}/reagent"

echo "Uploading new version file"
gsutil cp "system/version.txt" gs://re-agent

echo "Flushing cache"
gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent