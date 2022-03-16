.PHONY: build

build-all:
	scripts/build-all.sh

build-all-docker:
	docker rm -f agent_builder
	rm -f build/*

	docker build . -t agent-builder
	docker run \
	--name agent_builder \
	-v ${PWD}/build:/app/reagent/build \
	agent-builder

rollout: build-all-docker publish publish-version publish-latestVersions

publish:
	scripts/publish.sh

publish-version:
	gsutil cp "src/release/version.txt" gs://re-agent
	gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/version.txt

publish-latestVersions:
	gsutil cp "availableVersions.json" gs://re-agent
	gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/availableVersions.json