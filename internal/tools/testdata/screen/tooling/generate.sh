#!/bin/sh
# Explicit fixture maintenance only; never invoked by go test.
set -eu
cd "$(dirname "$0")/.."
docker build -t excalibur-screen-fixtures tooling
docker run --rm --memory=512m --cpus=1 --user "$(id -u):$(id -g)" -v "$PWD:/fixtures" excalibur-screen-fixtures
