.PHONY: build

# ${HOME}/go/bin/xgo -v -ldflags "-X 'reagent/system.BuildArch=armv5'" --targets=linux/arm-5 .
# mv reagent-linux-arm-5 build/reagent-linux-armv5
# ${HOME}/go/bin/xgo -v -ldflags "-X 'reagent/system.BuildArch=armv6'" --targets=linux/arm-6 .
# mv reagent-linux-arm-6 build/reagent-linux-armv6

build:
	${HOME}/go/bin/xgo -v -ldflags "-X 'reagent/system.BuildArch=armv5'" --targets=linux/arm-5 .
	mv reagent-linux-arm-5 build/reagent-linux-armv5
	${HOME}/go/bin/xgo -v -ldflags "-X 'reagent/system.BuildArch=armv6'" --targets=linux/arm-6 .
	mv reagent-linux-arm-6 build/reagent-linux-armv6
	node build.js --armv=7
	node build.js --arch=arm64
	node build.js --arch=amd64 --os=linux
	node build.js --arch=amd64 --os=darwin
	node build.js --arch=amd64 --os=windows


rollout: build publish

publish:
	./publish.sh

publish-version:
	gsutil cp "system/version.txt" gs://re-agent
	gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/version.txt
