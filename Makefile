build-dev:
	docker build . --target development --tag openslides-vote-dev

run-tests:
	docker build . --target testing --tag openslides-vote-test
	docker run openslides-vote-test

system-test:
	VOTE_SYSTEM_TEST=1 go test ./system_test/
