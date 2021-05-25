#!/bin/bash

version=`cat system/version.txt`

latestAgent="reagent-v${version}"

rm "reagent"
rm ${latestAgent} || true # Removes any old version first

mv reagent-linux-arm-7 reagent

echo "Uploading New Agent Binary"
gsutil cp "${latestAgent}" gs://re-agent/linux/armv7/${version}

echo "Uploading new version file"
gsutil cp "system/version.txt" gs://re-agent

echo "Updating latest binary"
gsutil cp "gs://re-agent/${latestAgent}" gs://re-agent/reagent-latest

echo "Flushing cache"
gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent