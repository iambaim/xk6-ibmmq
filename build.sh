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
    export CGO_CFLAGS="-I$MQ_INSTALLATION_PATH/tools/c/include -D_WIN64"
    export CGO_LDFLAGS="-L$MQ_INSTALLATION_PATH/bin64 -lmqm"
  fi
  echo $MQ_INSTALLATION_PATH
  echo $CGO_LDFLAGS
fi

go install go.k6.io/xk6/cmd/xk6@latest
XK6_RACE_DETECTOR=1 GCO_ENABLED=1 xk6 -v build \
    --with github.com/iambaim/xk6-ibmmq=.