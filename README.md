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

This project distributes source code only. Build your own `k6` binary or
container image locally.

To build a `k6` binary with this extension, first ensure you have the prerequisites:

- [Go 1.26 or later](https://go.dev/dl/)
- Git

Run `./build.sh` from the repository root. It uses xk6 v1.5.1 to build the k6 v2 version pinned in `go.mod` and writes the binary to `./k6`.

To build a local Linux x86-64 container image, run
`docker build --platform linux/amd64 -t xk6-ibmmq:local .`. The build downloads
the native MQ client directly from IBM. It is subject to the terms in the
download and is not part of this project's Apache-2.0 license.

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
Both scripts start the IBM MQ container and run
[`example/localtest.js`](./example/localtest.js).

### Migrating from simulated replies

Starting with xk6-ibmmq `v0.1.0`, `send()` no longer supports simulated replies.
Remove the fifth `simulateReply` argument from existing calls. If you passed
`true`, run a separate responder. `send()` puts one message on the source queue
and returns its message ID without consuming the request or generating a reply.

A nonempty reply queue now sends `MQMT_REQUEST` instead of `MQMT_DATAGRAM`.
Pass `''` as the reply queue for a one-way `MQMT_DATAGRAM`.

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

### Where the reply comes from

The Compose container loads [`example/echo.mqsc`](./example/echo.mqsc), which
configures IBM MQ to launch the `amqsech` sample when a request arrives on
`DEV.XK6.REQUEST`. There is no separate responder to start for this example.

1. The script sends `My Message` and its MQ message properties to
   `DEV.XK6.REQUEST`, with `ReplyToQ` set to `DEV.XK6.REPLY`. `send()` returns
   the request's message ID without waiting for the reply.
2. The test request queue uses `PROPCTL(NONE)`, so MQ delivers only the text
   body to `amqsech`. The responder echoes that text and copies the request's
   `MsgId` into the reply's `CorrelId`.
3. `receive()` uses `msgId` to pick that reply from `DEV.XK6.REPLY`, even if
   other replies are on the same queue. It waits up to 3,000 ms for a match.
   If none arrives, `receive()` returns `[0, '']` and the body check fails.

The snippet below shows one round trip. `example/localtest.js` uses a distinct
message for each virtual user and iteration so the check can catch a mix-up.

```javascript
import { check } from 'k6';
import ibmmq from 'k6/x/ibmmq';

ibmmq.newClient();

export default function () {
    const message = 'My Message';
    const properties = { apiVersion: 2, extraText: 'extra' };
    const msgId = ibmmq.send('DEV.XK6.REQUEST', 'DEV.XK6.REPLY', message, properties);
    const [rc, reply] = ibmmq.receive('DEV.XK6.REPLY', msgId, 3000);
    check(reply, { 'received matching echo': (body) => rc === 0 && body === message });
}
```

When testing your own application, use its queues in place of the `DEV.XK6.*`
queues. Your application takes the place of `amqsech` and must set the reply's
`CorrelId` to the request's `MsgId` for `receive()` to find it.

## Cut a release

Push a stable version tag (`vX.Y.Z`) for a commit on `main`. For example, for
version `v0.1.1`:

```sh
git switch main
git pull --ff-only
git tag -a v0.1.1 -m 'Release v0.1.1'
git push origin v0.1.1
```

The [Release workflow](https://github.com/iambaim/xk6-ibmmq/actions/workflows/release.yml)
builds the tagged commit on Linux and Windows and runs the MQ smoke tests on
Linux. If those checks pass, it publishes a GitHub release with generated notes
and source archives. It does not upload binaries or publish container images.

To publish an existing stable tag, run **Release** from `main` and enter the
tag. The workflow tests the tagged commit without moving or recreating the tag.

## Licenses

The extension source is licensed under [Apache-2.0](LICENSE). See
[third-party licenses](THIRD_PARTY_LICENSES.txt) for the texts and notices from
the pinned Go module graph and the xk6 build tool. k6 is licensed under
AGPLv3. The native IBM MQ client and the IBM MQ developer container come from
IBM and use IBM's separate terms.

After updating Go dependencies or the xk6 version, refresh the inventory with
`python3 scripts/update-third-party-licenses.py`.
