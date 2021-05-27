#!/bin/bash

version=`cat system/version.txt`

echo "Uploading New Agent Binary"
gsutil cp "reagent-linux-arm-7" "gs://re-agent/linux/armv7/${version}/reagent"

echo "Uploading new version file"
gsutil cp "system/version.txt" gs://re-agent

echo "Updating latest binary"
gsutil cp "gs://re-agent/linux/armv7/${version}/reagent" gs://re-agent/reagent-latest

echo "Flushing cache"
gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent