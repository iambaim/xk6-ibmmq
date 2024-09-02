#!/usr/bin/env bash
set -euo pipefail

export MQ_INSTALLATION_PATH=/opt/mqm

# Build
export CGO_ENABLED=1

# For github actions
if [[ ! -z ${GITHUB_RUN_ID+y} ]]; then
  export MQ_INSTALLATION_PATH=$HOME/IBM/MQ/data
  export CGO_CFLAGS="-I$MQ_INSTALLATION_PATH/inc"
  export CGO_LDFLAGS="-L$MQ_INSTALLATION_PATH/lib64 -Wl,-rpath,$MQ_INSTALLATION_PATH/lib64"
  echo $CGO_LDFLAGS
fi

go install go.k6.io/xk6/cmd/xk6@latest
xk6 build \
    --with github.com/iambaim/xk6-ibmmq=.

# Create ssl key
rm -fr ./pki
mkdir -p ./pki/keys/ibmwebspheremqqm1

$MQ_INSTALLATION_PATH/bin/runmqakm -keydb -create -db ./pki/myqmgr.kdb -type cms -stash -pw password
$MQ_INSTALLATION_PATH/bin/runmqakm -cert -create -db ./pki/myqmgr.kdb -stashed -label ibmwebspheremqqm1 -dn "CN=localhost,O=myOrganisation,OU=myDepartment,L=myLocation,C=NO" -sig_alg SHA384WithRSA
$MQ_INSTALLATION_PATH/bin/runmqakm -cert -extract -db ./pki/myqmgr.kdb -stashed -label ibmwebspheremqqm1 -target ./pki/tls.crt -format ascii
$MQ_INSTALLATION_PATH/bin/runmqakm -cert -export -db ./pki/myqmgr.kdb -stashed -label ibmwebspheremqqm1 -type cms -target ./pki/myqmgr2.p12 -target_stashed -target_type pkcs12
openssl pkcs12 -in ./pki/myqmgr2.p12 -nodes -nocerts -passin pass:password | openssl pkcs8 -nocrypt -out ./pki/tls.key 

cp ./pki/tls.* ./pki/keys/ibmwebspheremqqm1

# Run dev MQ container and wait until MQ is ready
docker compose -f example/docker-compose-ssl.yml up -d localmqtest
while curl --output /dev/null --silent --head --fail localhost:1414 ; [ $? -ne 52 ];do
  printf '.'
  sleep 1
done
sleep 5

# Run tests
export MQ_QMGR="QM1"
export MQ_CHANNEL="DEV.APP.SVRCONN"
export MQ_HOST="localhost"
export MQ_PORT=1414
export MQ_USERID="app"
export MQ_PASSWORD="password"

# For Linux/Windows only,
export MQ_TLS_KEYSTORE="./pki/myqmgr"
# for MacOS use the below
#export MQ_TLS_KEYSTORE="./pki/tls.crt"

# Also for MacOS we need to keep the VU number 1,
# perhaps a bug in the MQ client for MacOS?
./k6 run --vus 2 --duration 5s example/localtest.js

docker compose -f example/docker-compose-ssl.yml down