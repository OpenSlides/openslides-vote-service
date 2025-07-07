#!/bin/bash

# Executes all tests. Should errors occur, CATCH will be set to 1, causing an erroneous exit code.

echo "########################################################################"
echo "###################### Run Tests and Linters ###########################"
echo "########################################################################"

# Safe Exit
trap 'docker stop $(docker ps -a -q --filter ancestor=${IMAGE_TAG} --format="{{.ID}}")' EXIT

# Setup
IMAGE_TAG=openslides-vote-tests

# Execution
make build-test
docker run --privileged -t ${IMAGE_TAG} ./dev/container-tests.sh