import { check } from 'k6';
import { vu } from 'k6/execution';
import ibmmq from 'k6/x/ibmmq';

const vus = Number(__ENV.MQ_TEST_VUS || 2);
const iterations = 5;
const reuse = __ENV.MQ_CONNECTION_REUSE === 'true';
const connectionsPerVU = reuse ? 1 : iterations * 2;
const thresholds = {
    checks: ['rate==1'],
    iterations: [`count==${vus * iterations}`],
    ibmmq_connections: [`count==${vus * connectionsPerVU}`],
};

for (let id = 1; id <= vus; id++) {
    thresholds[`ibmmq_connections{mq_test_vu:${id}}`] = [`count==${connectionsPerVU}`];
}

export const options = {
    scenarios: {
        roundTrip: { executor: 'per-vu-iterations', vus, iterations },
    },
    thresholds,
};

ibmmq.newClient();

export function setup() {
    const msgId = ibmmq.send('DEV.XK6.NO.RESPONDER', 'DEV.XK6.REPLY', 'No responder', { apiVersion: 2 });
    const [rc, reply] = ibmmq.receive('DEV.XK6.REPLY', msgId, 100);
    if (!/^[0-9a-f]{48}$/.test(msgId) || rc !== 0 || reply !== '') {
        throw new Error('Expected send() to return a message ID without generating a reply');
    }
}

export default function () {
    vu.metrics.tags.mq_test_vu = String(vu.idInTest);
    const sourceQueue = 'DEV.XK6.REQUEST';
    const replyQueue = 'DEV.XK6.REPLY';
    const sourceMessage = `Request from VU ${vu.idInTest}, iteration ${vu.iterationInScenario}`;
    const properties = { apiVersion: 2, extraText: 'extra' };
    let matched = false;
    try {
        const msgId = ibmmq.send(sourceQueue, replyQueue, sourceMessage, properties);
        const [rc, reply] = ibmmq.receive(replyQueue, msgId, 3000);
        matched = rc === 0 && reply === sourceMessage;
        if (reuse && vu.iterationInScenario === 0) {
            const [timeoutRC, missing] = ibmmq.receive(replyQueue, msgId, 100);
            matched = matched && timeoutRC === 0 && missing === '';
        }
        if (reuse && vu.iterationInScenario === iterations - 1) {
            ibmmq.close();
        }
    } finally {
        check(matched, { 'received matching echo': (ok) => ok });
    }
}
