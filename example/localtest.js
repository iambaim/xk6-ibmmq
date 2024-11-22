import { check } from 'k6';
import ibmmq from 'k6/x/ibmmq';

const rc = ibmmq.newClient()

export default function () {
    const sourceQueue = "DEV.QUEUE.1"
    const replyQueue = "DEV.QUEUE.2"
    const sourceMessage = "Sent Message"
    // Below is the maximum waiting time to wait for the reply
    const waitInterval = 3 * 1000
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

    const msgId = ibmmq.send(sourceQueue, replyQueue, sourceMessage, extraProperties, simulateReply)
    const [rc, res] = ibmmq.receive(replyQueue, msgId, waitInterval)

    // Check the results
    // If simulateReply = true, the reply message text is always "Reply Message"
    const replyMessage = "Reply Message"
    check(res, {
        'Verify reply text': (r) => r == replyMessage,
    });
    check(rc, {
        'Verify return code': (r) => r == 0,
    });
}
