#!/bin/sh

if [ ! -z $dev   ]; then CompileDaemon -log-prefix=false -build="go build -o vote-service ./openslides-vote-service" -command="./vote-service"; fi
if [ ! -z $tests ]; then go test -test.short -race -timeout 12s ./...; fi
# No Production Command