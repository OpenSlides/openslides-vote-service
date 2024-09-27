build-dev:
	rm -fr openslides-autoupdate-service
	cp -r ../openslides-autoupdate-service .
	docker build . --target development --tag openslides-vote-dev
	rm -fr openslides-autoupdate-service

run-tests:
	docker build . --target testing --tag openslides-vote-test
	docker run openslides-vote-test

system-test:
	VOTE_SYSTEM_TEST=1 go test ./system_test/
