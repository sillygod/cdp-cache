#!/bin/bash

PROJECT_PATH=$(cd "$(dirname "${BASH_SOURCE[0]}")"; pwd -P)
go test -coverprofile=/tmp/c.out ./...
go tool cover -html=/tmp/c.out
