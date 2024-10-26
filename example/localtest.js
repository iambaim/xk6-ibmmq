import ibmmq from 'k6/x/ibmmq';

const rc = ibmmq.newClient()

export default function () {
    const sourceQueue = "DEV.QUEUE.1"
    const replyQueue = "DEV.QUEUE.2"
    const sourceMessage = "Sent Message"
    const replyMessage = "Reply Message"
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
    ibmmq.receive(replyQueue, msgId, replyMessage)
}
