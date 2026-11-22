# xk6-ibmmq

[k6 v2](https://grafana.com/docs/k6/latest/) extension for [IBM MQ](https://www.ibm.com/products/mq) using the [xk6](https://github.com/grafana/xk6)
system.

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

- [Go 1.26 or later](https://go.dev/dl/)
- Git

Run `./build.sh` from the repository root. It uses xk6 v1.5.1 to build the k6 v2 version pinned in `go.mod` and writes the binary to `./k6`.

## IBM MQ settings

This plugin uses the IBM MQ Golang package (https://github.com/ibm-messaging/mq-golang).
To configure the MQ connection factory, you need to set these environment variables:

1. `MQ_QMGR`. Queue manager name (e.g., "QM1").
2. `MQ_CHANNEL`. Channel name (e.g., "DEV.APP.SVRCONN").
3. `MQ_HOST`. Host name to connect to (e.g., "localhost").
4. `MQ_PORT`. Port number to connect to (e.g., 1414).
5. `MQ_USERID`. User ID to use.
6. `MQ_PASSWORD`. User password to use.
7. `MQ_TLS_KEYSTORE`. TLS keystore to use. Usage example: [local-test-ssl.sh](./local-test-ssl.sh).
8. `MQ_TLS_CIPHER_SPEC`. TLS CipherSpec to use. Usage example: [local-test-ssl.sh](./local-test-ssl.sh).

## Run local test

Run `./build.sh`, then `./local-test.sh` or `./local-test-ssl.sh` for TLS.
Both scripts start an IBM MQ container with a test-only echo responder and run
[`example/localtest.js`](./example/localtest.js). The responder echoes the
request body and uses the request's message ID as the reply's correlation ID.

### Migrating from simulated replies

Remove the fifth `simulateReply` argument from `send()`. If you used `true`,
start a separate responder before running your test. `send()` puts one message
on the source queue and returns its message ID; it no longer consumes the
request or generates a reply.

A nonempty reply queue sets the MQ message type to `MQMT_REQUEST`. Pass `''`
for a one-way `MQMT_DATAGRAM`, and pass `null` if there are no message properties.

## Example test

```bash
$ docker compose -f example/docker-compose.yml up -d --wait localmqtest
$ export MQ_QMGR="QM1"
$ export MQ_CHANNEL="DEV.APP.SVRCONN"
$ export MQ_HOST="localhost"
$ export MQ_PORT=1414
$ export MQ_USERID="app"
$ export MQ_PASSWORD="password"

$ ./k6 run example/localtest.js
$ docker compose -f example/docker-compose.yml down
```

```javascript
import { check } from 'k6';
import ibmmq from 'k6/x/ibmmq';

ibmmq.newClient();

export default function () {
    const message = 'My Message';
    const msgId = ibmmq.send('DEV.XK6.REQUEST', 'DEV.XK6.REPLY', message, null);
    const [rc, reply] = ibmmq.receive('DEV.XK6.REPLY', msgId, 3000);
    check(reply, { 'received matching echo': (body) => rc === 0 && body === message });
}
```

### Send with message properties

Pass a JavaScript object as the fourth argument to `send()` to attach IBM MQ
message properties. This one-way example uses `DEV.QUEUE.1` from the local
container and leaves the reply queue blank.

```javascript
import ibmmq from 'k6/x/ibmmq';

ibmmq.newClient();

export default function () {
    ibmmq.send('DEV.QUEUE.1', '', 'Order shipped', {
        apiVersion: 2,
        extraText: 'extra',
    });
}
```

The echo test passes `null` because IBM's sample responder can include the
MQ property header in its reply, which would break the plain-text comparison.

## Cut a release

After merging to `main`, tag the commit you want to release and push the tag.
For example, for version `v0.1.0`:

```sh
git switch main
git pull --ff-only
git tag -a v0.1.0 -m 'Release v0.1.0'
git push origin v0.1.0
```

The Release workflow runs the build and smoke tests, then creates a draft
GitHub release with Linux and Windows x86-64 archives and `SHA256SUMS`. The
archives do not bundle the IBM MQ client; install it before running the binary.
Tags such as `v0.1.0-rc.1` are marked as prereleases.

Review the draft's notes and assets, then publish it in GitHub. Publishing
the draft builds and pushes the Linux x86-64 container image with the same tag,
for example `ghcr.io/iambaim/xk6-ibmmq:v0.1.0`. The image bundles the IBM MQ
runtime, so no client installation is needed on the host.
