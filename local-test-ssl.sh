#!/usr/bin/env bash
set -euo pipefail

# Create ssl key
rm -fr ./pki
mkdir -p ./pki/keys/ibmwebspheremqqm1

if [[ ! -z ${GITHUB_RUN_ID+y} ]]; then
  export MQ_INSTALLATION_PATH=$HOME/IBM/MQ/data
fi

"$MQ_INSTALLATION_PATH/bin/runmqakm" -keydb -create -db ./pki/myqmgr.kdb -type cms -stash -pw password
"$MQ_INSTALLATION_PATH/bin/runmqakm" -cert -create -db ./pki/myqmgr.kdb -stashed -label ibmwebspheremqqm1 -dn "CN=localhost,O=myOrganisation,OU=myDepartment,L=myLocation,C=NO"
"$MQ_INSTALLATION_PATH/bin/runmqakm" -cert -extract -db ./pki/myqmgr.kdb -stashed -label ibmwebspheremqqm1 -target ./pki/tls.crt -format ascii
"$MQ_INSTALLATION_PATH/bin/runmqakm" -cert -export -db ./pki/myqmgr.kdb -stashed -label ibmwebspheremqqm1 -type cms -target ./pki/myqmgr2.p12 -target_stashed -target_type pkcs12
openssl pkcs12 -in ./pki/myqmgr2.p12 -nodes -nocerts -passin pass:password | openssl pkcs8 -nocrypt -out ./pki/tls.key 

cp ./pki/tls.* ./pki/keys/ibmwebspheremqqm1

# Run dev MQ container and wait until MQ is ready
compose=(docker compose -f example/docker-compose-ssl.yml)
trap '"${compose[@]}" down' EXIT
"${compose[@]}" up -d --wait --wait-timeout 180 localmqtest

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

export MQ_TLS_CIPHER_SPEC="ANY_TLS12_OR_HIGHER"

MQ_TEST_VUS=2 MQ_TEST_EXPECT_REUSE=false MQ_CONNECTION_REUSE=false "${K6_BINARY:-./k6}" run example/localtest.js
MQ_TEST_VUS=2 MQ_TEST_EXPECT_REUSE=true MQ_CONNECTION_REUSE=true "${K6_BINARY:-./k6}" run example/localtest.js
