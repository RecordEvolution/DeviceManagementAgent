.PHONY: build

ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

help: ## This help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help

run:
	cd src && sudo go run . -config test-config.flock -prettyLogging -env=local -prettyLogging

run_mac:
	cd src && DOCKER_HOST=unix://${HOME}/Library/Containers/com.docker.docker/Data/docker.raw.sock go run . -config test-config.flock -prettyLogging -env=local -nmw=false

# Not preferred. Use build-all-docker instead.
build-all:
	scripts/build-all.sh

build-all-docker: clean ## Builds all docker images for all targets in targets files
	docker build --platform linux/amd64 . -t agent-builder
	docker run --name agent_builder -v ${ROOT_DIR}/build:/app/reagent/build agent-builder

rollout: build-all-docker publish release ## Do everythin in one step

clean:
	docker rm -f agent_builder
	rm -f build/*

release: publish-version publish-latestVersions ## publish the new metadata

publish: # publish the binaries from the build folder
	scripts/publish.sh

publish-version:
	gsutil cp "src/release/version.txt" gs://re-agent
	gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/version.txt

publish-latestVersions:
	gsutil cp "availableVersions.json" gs://re-agent
	gsutil setmeta -r -h "Cache-control:public, max-age=0" gs://re-agent/availableVersions.json