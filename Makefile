override SERVICE=vote
override MAKEFILE_PATH=../dev/scripts/makefile
override DOCKER_COMPOSE_FILE=

# Build images for different contexts

build build-prod build-dev build-tests:
	bash $(MAKEFILE_PATH)/make-build-service.sh $@ $(SERVICE)

# Development

run-dev run-dev-standalone run-dev-attached run-dev-detached run-dev-help run-dev-stop run-dev-clean run-dev-exec run-dev-enter:
	bash $(MAKEFILE_PATH)/make-run-dev.sh "$@" "$(SERVICE)" "$(DOCKER_COMPOSE_FILE)" "$(ARGS)"

# Tests

run-tests:
	bash dev/run-tests.sh

run-lint:
	gofmt -l -s -w .
	go test ./...
	golint -set_exit_status ./...

system-test:
	VOTE_SYSTEM_TEST=1 go test ./system_test/
