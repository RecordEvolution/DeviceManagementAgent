build-arm7:
	rm reagent-linux-arm-7 || true && ${GOPATH}/bin/xgo -v --targets=linux/arm-7 .

publish-arm7:
	./publish-arm7.sh

build-arm64:
	rm reagent-linux-arm64 || true && ${GOPATH}/bin/xgo -v --targets=linux/arm64 .

rollout-arm7:
	make build-arm7 && make publish-arm7

publish-version:
	gsutil cp "system/version.txt" gs://re-agent
	gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/version.txt