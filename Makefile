override SERVICE=vote

# Build images for different contexts

build-prod:
	docker build ./ $(ARGS) --tag "openslides-$(SERVICE)" --build-arg CONTEXT="prod" --target "prod"

build-dev:
	docker build ./ $(ARGS) --tag "openslides-$(SERVICE)-dev" --build-arg CONTEXT="dev" --target "dev"

build-tests:
	docker build ./ $(ARGS) --tag "openslides-$(SERVICE)-tests" --build-arg CONTEXT="tests" --target "tests"

# Tests

run-tests:
	bash dev/run-tests.sh

lint:
	bash dev/run-lint.sh -l

system-test:
	VOTE_SYSTEM_TEST=1 go test ./system_test/

gofmt:
	gofmt -l -s -w .