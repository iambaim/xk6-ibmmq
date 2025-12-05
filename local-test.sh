#!/usr/bin/env bash
set -euo pipefail

# Run dev MQ container and wait until MQ is ready
docker compose -f example/docker-compose.yml up -d localmqtest
while curl --output /dev/null --silent --head --fail localhost:1414 ; [ $? -ne 52 ];do
  printf '.'
  sleep 5
done

# Run tests
export MQ_QMGR="QM1"
export MQ_CHANNEL="DEV.APP.SVRCONN"
export MQ_HOST="localhost"
export MQ_PORT=1414
export MQ_USERID="app"
export MQ_PASSWORD="password"

./k6 run --vus 2 --duration 5s example/localtest.js

docker compose -f example/docker-compose.yml down