#!/bin/bash

version=`cat system/version.txt`

latestAgent="reagent-v${version}"

rm ${latestAgent} || true # Removes any old version first

mv reagent-linux-arm-7 "${latestAgent}"

echo "Uploading New Agent Binary"
gsutil cp "${latestAgent}" gs://re-agent

echo "Uploading new version file"
gsutil cp "system/version.txt" gs://re-agent

echo "Updating latest binary"
gsutil cp "gs://re-agent/${latestAgent}" gs://re-agent/reagent-latest

echo "Flushing cache"
gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent