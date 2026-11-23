#!/usr/bin/env bash
set -euo pipefail

# Run dev MQ container and wait until MQ is ready
compose=(docker compose -f example/docker-compose.yml)
trap '"${compose[@]}" down' EXIT
"${compose[@]}" up -d --wait --wait-timeout 180 localmqtest

# Run tests
export MQ_QMGR="QM1"
export MQ_CHANNEL="DEV.APP.SVRCONN"
export MQ_HOST="localhost"
export MQ_PORT=1414
export MQ_USERID="app"
export MQ_PASSWORD="password"

MQ_TEST_VUS=2 MQ_TEST_EXPECT_REUSE=false MQ_CONNECTION_REUSE=false "${K6_BINARY:-./k6}" run example/localtest.js
MQ_TEST_VUS=2 MQ_TEST_EXPECT_REUSE=true MQ_CONNECTION_REUSE=1 "${K6_BINARY:-./k6}" run example/localtest.js
MQ_TEST_VUS=8 MQ_TEST_EXPECT_REUSE=true MQ_CONNECTION_REUSE=false "${K6_BINARY:-./k6}" run -e MQ_CONNECTION_REUSE=true example/localtest.js
