#!/usr/bin/env bash
set -euo pipefail

# Build

# For github actions
if [[ ! -z ${GITHUB_RUN_ID+y} ]]; then
  export MQ_INSTALLATION_PATH=$HOME/IBM/MQ/data
  if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    export CGO_CFLAGS="-I$MQ_INSTALLATION_PATH/inc"
    export CGO_LDFLAGS="-L$MQ_INSTALLATION_PATH/lib64 -Wl,-rpath,$MQ_INSTALLATION_PATH/lib64"
  elif [[ "$OSTYPE" == "msys" ]]; then
    export CGO_CFLAGS="-I$MQ_INSTALLATION_PATH/tools/c/include"
    export CGO_LDFLAGS="-L$MQ_INSTALLATION_PATH/tools/Lib64 -Wl,-rpath,$MQ_INSTALLATION_PATH/tools/Lib64"
  fi
  echo $MQ_INSTALLATION_PATH
  echo $CGO_LDFLAGS
fi

echo $MQ_INSTALLATION_PATH
ls $MQ_INSTALLATION_PATH

go install go.k6.io/xk6/cmd/xk6@latest
XK6_RACE_DETECTOR=1 GCO_ENABLED=1 xk6 -v build \
    --with github.com/iambaim/xk6-ibmmq=.

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