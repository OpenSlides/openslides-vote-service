override SERVICE=vote
override MAKEFILE_PATH=../dev/scripts/makefile
override DOCKER_COMPOSE_FILE=

# Build images for different contexts

build-prod:
	docker build ./ --tag "openslides-$(SERVICE)" --build-arg CONTEXT="prod" --target "prod"

build-dev:
	docker build ./ --tag "openslides-$(SERVICE)-dev" --build-arg CONTEXT="dev" --target "dev"

build-tests:
	docker build ./ --tag "openslides-$(SERVICE)-tests" --build-arg CONTEXT="tests" --target "tests"

# Development

.PHONY: dev%

dev%:
	bash $(MAKEFILE_PATH)/make-dev.sh "$@" "$(SERVICE)" "$(DOCKER_COMPOSE_FILE)" "$(ARGS)" "$(USED_SHELL)"

# Tests

run-tests:
	bash dev/run-tests.sh

lint:
	bash dev/run-lint.sh -l

system-test:
	VOTE_SYSTEM_TEST=1 go test ./system_test/

gofmt:
	gofmt -l -s -w .