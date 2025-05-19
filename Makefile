build-aio:
	@if [ -z "${submodule}" ] ; then \
		echo "Please provide the name of the submodule service to build (submodule=<submodule service name>)"; \
		exit 1; \
	fi

	@if [ "${context}" != "prod" -a "${context}" != "dev" -a "${context}" != "tests" ] ; then \
		echo "Please provide a context for this build (context=<desired_context> , possible options: prod, dev, tests)"; \
		exit 1; \
	fi

	echo "Building submodule '${submodule}' for ${context} context"

	@docker build -f ./Dockerfile.AIO ./ --tag openslides-${submodule}-${context} --build-arg CONTEXT=${context} --target ${context} ${args}

build-dev:
	make build-aio context=dev submodule=vote
#	docker build . --target development --tag openslides-vote-dev

run-tests:
	make build-aio context=tests submodule=vote
	docker run openslides-vote-tests
#   docker build . --target testing --tag openslides-vote-test

system-test:
	VOTE_SYSTEM_TEST=1 go test ./system_test/
