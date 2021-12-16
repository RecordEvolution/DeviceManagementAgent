.PHONY: build

build:
	node build.js --armv=6
	node build.js --armv=7
	${HOME}/go/bin/xgo -v -ldflags "-X 'reagent/system.BuildArch=arm64'" --targets=linux/arm64 .
	true || mv reagent-linux-arm64 build/reagent-linux-arm64
	node build.js --arch=amd64 --os=linux
	${HOME}/go/bin/xgo -v -ldflags "-X 'reagent/system.BuildArch=amd64'" --targets=darwin/amd64 .
	true || mv reagent-darwin-10.12-amd64 build/reagent-darwin-amd64
	node build.js --arch=amd64 --os=windows


rollout: build publish publish-version

publish:
	./publish.sh

publish-version:
	gsutil cp "system/version.txt" gs://re-agent
	gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/version.txt
