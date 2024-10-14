build-dev:
	docker build . --target development --tag openslides-vote-dev

build-dev-fullstack:
	DOCKER_BUILDKIT=1 docker build . --target development-fullstack --build-context autoupdate=../openslides-autoupdate-service --tag openslides-vote-dev-fullstack

run-tests:
	docker build . --target testing --tag openslides-vote-test
	docker run openslides-vote-test

system-test:
	VOTE_SYSTEM_TEST=1 go test ./system_test/
