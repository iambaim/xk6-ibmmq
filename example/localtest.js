import { check } from 'k6';
import { vu } from 'k6/execution';
import ibmmq from 'k6/x/ibmmq';

export const options = {
    scenarios: {
        roundTrip: { executor: 'per-vu-iterations', vus: 2, iterations: 5 },
    },
    thresholds: { checks: ['rate==1'], iterations: ['count==10'] },
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
    const sourceQueue = 'DEV.XK6.REQUEST';
    const replyQueue = 'DEV.XK6.REPLY';
    const sourceMessage = `Request from VU ${vu.idInTest}, iteration ${vu.iterationInScenario}`;
    const properties = { apiVersion: 2, extraText: 'extra' };
    let matched = false;
    try {
        const msgId = ibmmq.send(sourceQueue, replyQueue, sourceMessage, properties);
        const [rc, reply] = ibmmq.receive(replyQueue, msgId, 3000);
        matched = rc === 0 && reply === sourceMessage;
    } finally {
        check(matched, { 'received matching echo': (ok) => ok });
    }
}
