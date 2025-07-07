#!/bin/sh

dockerd &

# Close Dockerd savely on exit
DOCKERD_PID=$!
trap 'kill $DOCKERD_PID' EXIT INT TERM ERR

RETRY=0
until docker info  >/dev/null 2>&1; do
  if [ "$RETRY" -ge 10 ]; then
    echo "Dockerd setup error"
    exit 1
  fi
  echo "Waiting for dockerd"
  sleep 1
  RETRY=$(tries + 1)
done

echo "Started dockerd"

# Run Linters & Tests
go vet ./...
gofmt -l .
golint -set_exit_status ./...
go test -timeout 60s -race ./...