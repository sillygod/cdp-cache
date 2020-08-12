#!/bin/bash

PROJECT_PATH=$(cd "$(dirname "${BASH_SOURCE[0]}")"; pwd -P)
rm -f /tmp/c.out
go test -v -coverprofile=/tmp/c.out ./...
go tool cover -html=/tmp/c.out
