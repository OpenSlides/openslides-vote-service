#!/bin/bash

# Executes all tests. Should errors occur, CATCH will be set to 1, causing an erroneous exit code.

echo "########################################################################"
echo "###################### Run Tests and Linters ###########################"
echo "########################################################################"

# Parameters
while getopts "p" FLAG; do
    case "${FLAG}" in
    p) PERSIST_CONTAINERS=true ;;
    s) SKIP_BUILD=true ;;
    *) echo "Can't parse flag ${FLAG}" && break ;;
    esac
done

# Setup
IMAGE_TAG=openslides-vote-tests
LOCAL_PWD=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
CATCH=0

# Execution
if [ -z "$SKIP_BUILD" ]; then make build-tests || CATCH=1; fi
docker run -d -v /var/run/docker.sock:/var/run/docker.sock --name vote-test ${IMAGE_TAG} || CATCH=1
docker exec vote-test go vet ./... || CATCH=1
docker exec vote-test go test -test.short ./... || CATCH=1

# Linters
bash "$LOCAL_PWD"/run-lint.sh -s -c || CATCH=1

if [ -z "$PERSIST_CONTAINERS" ]; then docker stop vote-test && docker rm vote-test || CATCH=1; fi

exit $CATCH