#!/bin/bash

PROJECT_PATH=$(cd "$(dirname "${BASH_SOURCE[0]}")"; pwd -P)

if ! [ -e /tmp/caddy-benchmark ]
then
    mkdir -p /tmp/caddy-benchmark
fi

cp -a $PROJECT_PATH/test_data/* /tmp/caddy-benchmark/
