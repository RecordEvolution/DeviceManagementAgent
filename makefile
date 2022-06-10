.PHONY: build

help: ## This help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help

build-all:
	scripts/build-all.sh

build-all-docker: clean ## Builds all docker images for all targets in targets files
	cd src && go mod tidy
	docker build . -t agent-builder
	docker run --name agent_builder -v ${PWD}/build:/app/reagent/build agent-builder

rollout: build-all-docker publish publish-version publish-latestVersions ## Do everythin in one step

clean:
	docker rm -f agent_builder
	rm -f build/*

publish-all: publish publish-version publish-latestVersions ## publish the metadata and binaries from the build folder

publish:
	scripts/publish.sh

publish-version:
	gsutil cp "src/release/version.txt" gs://re-agent
	gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/version.txt

publish-latestVersions:
	gsutil cp "availableVersions.json" gs://re-agent
	gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/availableVersions.json