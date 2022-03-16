#!/bin/bash

array=($(ls build))
version=`cat release/version.txt`

for element in "${array[@]}"; do
    OS=$(echo "$element" | cut -d "-" -f 2)
    ARCH=$(echo "$element" | cut -d "-" -f 3)
    GCLOUD="gs://re-agent/${OS}/${ARCH}/${version}/reagent"
    gsutil cp "build/$element" $GCLOUD
done

gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent