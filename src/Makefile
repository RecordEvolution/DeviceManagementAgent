build-arm7:
	rm reagent-linux-arm-7 || true && ${GOPATH}/bin/xgo -v -ldflags "-X 'reagent/system.BuildArch=armv7'" --targets=linux/arm-7 .

build-arm6:
	rm reagent-linux-arm-6 || true && ${GOPATH}/bin/xgo -v  -ldflags "-X 'reagent/system.BuildArch=armv6'" --targets=linux/arm-6 .

build-arm5:
	rm reagent-linux-arm-5 || true && ${GOPATH}/bin/xgo -v -ldflags "-X 'reagent/system.BuildArch=armv5'" --targets=linux/arm-5 .

build-arm64:
	./build-arm64.sh

publish-arm64:
	./publish-arm64.sh

publish-arm7:
	./publish-arm7.sh

publish-amd64:
	./publish-amd64.sh

rollout-arm7:
	make build-arm7 && make publish-arm7

rollout-amd64:
	./build-amd64.sh && ./publish-amd64.sh

rollout-arm64:
	./build-arm64.sh && ./publish-arm64.sh

publish-version:
	gsutil cp "system/version.txt" gs://re-agent
	gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/version.txt