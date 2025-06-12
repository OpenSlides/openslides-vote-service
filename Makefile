SERVICE=vote 

build-dev:
	bash ../dev/scripts/makefile/build-service.sh $(SERVICE) dev

build-prod:
	bash ../dev/scripts/makefile/build-service.sh $(SERVICE) prod

build-test:
	bash ../dev/scripts/makefile/build-service.sh $(SERVICE) tests

run-tests:
	bash dev/run-tests.sh

run-test:
	make build-aio context=tests submodule=vote
	docker run openslides-vote-tests
#   docker build . --target testing --tag openslides-vote-test

system-test:
	VOTE_SYSTEM_TEST=1 go test ./system_test/
