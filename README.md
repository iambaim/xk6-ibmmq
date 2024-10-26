# xk6-ibmmq

[k6](https://go.k6.io/k6) extension for [IBM MQ](https://www.ibm.com/products/mq) using the [xk6](https://github.com/grafana/xk6)
system.

| :exclamation: This is still a very rudimentary implementation. Breaking changes are expected. USE AT YOUR OWN RISK! |
|------|

## Prerequisites

IBM MQ client (Linux/Win) or toolkit (Mac) installation is required to build and run this extension. 

1. Linux/Windows:

   Download the client installer from: https://public.dhe.ibm.com/ibmdl/export/pub/software/websphere/messaging/mqdev/redist

   More information: https://www.ibm.com/docs/en/ibm-mq/9.4?topic=overview-redistributable-mq-clients

2. MacOS:

   Download the client installer from: https://public.dhe.ibm.com/ibmdl/export/pub/software/websphere/messaging/mqdev/mactoolkit

   More information: https://ibm.biz/mqdevmacclient

Or take advantage of this: https://github.com/marketplace/actions/setup-mq-client, to build and run using Github Actions.

## Build

To build a `k6` binary with this extension, first ensure you have the prerequisites:

- [Go toolchain](https://go101.org/article/go-toolchain.html)
- Git

Then, install [xk6](https://github.com/grafana/xk6) and build your custom k6 binary with the IBM MQ extension:

1. Install `xk6`:

  ```shell
  $ go install go.k6.io/xk6/cmd/xk6@latest
  ```

2. Build the binary:

  ```shell
  $ export CGO_ENABLED=1
  $ xk6 build --with github.com/iambaim/xk6-ibmmq@latest
  ```

## IBM MQ settings

This plugin uses the IBM MQ JMS 2.0 in Golang interface (https://github.com/ibm-messaging/mq-golang-jms20).
To configure the MQ connection factory, you need to set these environment variables:

1. `MQ_QMGR`. Queue manager name (e.g., "QM1").
2. `MQ_CHANNEL`. Channel name (e.g., "DEV.APP.SVRCONN").
3. `MQ_HOST`. Host name to connect to (e.g., "localhost").
4. `MQ_PORT`. Port number to connect to (e.g., 1414).
5. `MQ_USERID`. User ID to use.
6. `MQ_PASSWORD`. User password to use.
7. `MQ_TLS_KEYSTORE`. **(NEW)** TLS keystore to use. Usage example: [local-test-ssl.sh](./local-test-ssl.sh).

## Run local test

Use the provided `./local-test.sh` and `./local-test-ssl.sh` (for SSL). 
These files are the self-contained test script that will spin up a local MQ
container using docker, build the extension, and run an example k6 test with
IBM MQ in `./example/localtest.js` file.

## Example test

```bash
$ export MQ_QMGR="QM1"
$ export MQ_CHANNEL="DEV.APP.SVRCONN"
$ export MQ_HOST="localhost"
$ export MQ_PORT=1414
$ export MQ_USERID="app"
$ export MQ_PASSWORD="password"

$ ./k6 run --vus 2 --duration 5s example/localtest.js
```

```javascript
import ibmmq from 'k6/x/ibmmq';

const client = ibmmq.newClient()

export default function () {
    const sourceQueue = "DEV.QUEUE.1"
    const replyQueue = "DEV.QUEUE.2"
    const sourceMessage = "My Message"
    const replyMessage = "ReplyMsg"
    // Below is the extra properties that we want to set
    // Leave it as null or an empty map if no extra properties are needed
    const extraProperties = new Map([
        ["apiVersion", 2],
        ["extraText", "extra"]
    ])
    // The below parameter enable/disable a simulated application that will consume
    // the message in the source queue and put a reply message in the reply queue
    // and the reply message correlation ID == source message ID
    const simulateReply = true

    const msgID = ibmmq.send(client, sourceQueue, replyQueue, sourceMessage, simulateReply)
    ibmmq.receive(client, replyQueue, msgID, replyMessage)
}
```