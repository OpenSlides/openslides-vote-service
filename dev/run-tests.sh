#!/bin/bash

# Executes all tests. Should errors occur, CATCH will be set to 1, causing an erroneous exit code.

echo "########################################################################"
echo "###################### Run Tests and Linters ###########################"
echo "########################################################################"

# Safe Exit
trap 'docker stop $(docker ps -a -q --filter ancestor=${IMAGE_TAG} --format="{{.ID}}")' EXIT INT TERM ERR

# Setup
IMAGE_TAG=openslides-vote-tests

# Execution
if [ "$(docker images -q $IMAGE_TAG)" = "" ]; then make build-test; fi
docker run --privileged ${IMAGE_TAG}