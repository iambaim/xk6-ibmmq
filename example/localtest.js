import ibmmq from 'k6/x/ibmmq';

const rc = ibmmq.newClient()

export default function () {
    const sourceQueue = "DEV.QUEUE.1"
    const replyQueue = "DEV.QUEUE.2"
    const sourceMessage = "Sent Message"
    const replyMessage = "Reply Message"
    // The below parameter enable/disable a simulated application that will consume
    // the message in the source queue and put a reply message in the reply queue
    // and the reply message correlation ID == source message ID
    const simulateReply = true

    const msgId = ibmmq.send(sourceQueue, replyQueue, sourceMessage, simulateReply)
    ibmmq.receive(replyQueue, msgId, replyMessage)
}
