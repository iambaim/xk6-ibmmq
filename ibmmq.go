package xk6ibmmq

import (
	"encoding/hex"
	"github.com/ibm-messaging/mq-golang/v5/ibmmq"
	"github.com/walles/env"
	"go.k6.io/k6/js/modules"
	"log"
	"strconv"
)

func init() {
	modules.Register("k6/x/ibmmq", new(Ibmmq))
}

type Ibmmq struct {
	QMName string
	cno    *ibmmq.MQCNO
}

func (s *Ibmmq) NewClient() int {
	var qMgr ibmmq.MQQueueManager
	var err error
	var rc int

	QMName := env.MustGet("MQ_QMGR", env.String)
	Hostname := env.MustGet("MQ_HOST", env.String)
	PortNumber := env.MustGet("MQ_PORT", env.String)
	ChannelName := env.MustGet("MQ_CHANNEL", env.String)
	UserName := env.MustGet("MQ_USERID", env.String)
	Password := env.MustGet("MQ_PASSWORD", env.String)

	// Allocate the MQCNO and MQCD structures needed for the CONNX call.
	cno := ibmmq.NewMQCNO()
	cd := ibmmq.NewMQCD()

	// Fill in required fields in the MQCD channel definition structure
	cd.ChannelName = ChannelName
	cd.ConnectionName = Hostname + "(" + PortNumber + ")"

	// Reference the CD structure from the CNO and indicate that we definitely want to
	// use the client connection method.
	cno.ClientConn = cd
	cno.Options = ibmmq.MQCNO_CLIENT_BINDING

	// MQ V9.1.2 allows applications to specify their own names. This is ignored
	// by older levels of the MQ libraries.
	cno.ApplName = "xk6-ibmmq"

	if UserName != "" {
		csp := ibmmq.NewMQCSP()
		csp.AuthenticationType = ibmmq.MQCSP_AUTH_USER_ID_AND_PWD
		csp.UserId = UserName
		csp.Password = Password

		// Make the CNO refer to the CSP structure so it gets used during the connection
		cno.SecurityParms = csp
	}

	// And now we can try to connect. Wait a short time before disconnecting.
	qMgr, err = ibmmq.Connx(QMName, cno)
	if err == nil {
		defer qMgr.Disc()
		s.QMName = QMName
		s.cno = cno
		rc = 0
	} else {
		rc = int(err.(*ibmmq.MQReturn).MQCC)
		log.Fatal("Error during Connect: " + strconv.Itoa(rc) + err.Error())
	}
	return rc
}

func (s *Ibmmq) Connect() ibmmq.MQQueueManager {
	qMgr, err := ibmmq.Connx(s.QMName, s.cno)
	if err != nil {
		rc := int(err.(*ibmmq.MQReturn).MQCC)
		log.Fatal("Error during Connect: " + strconv.Itoa(rc) + err.Error())
	}
	return qMgr
}

func (s *Ibmmq) Send(sourceQueue string, replyQueue string, sourceMessage string, simulateReply bool) string {
	var msgId string
	var qMgr ibmmq.MQQueueManager

	mqod := ibmmq.NewMQOD()
	openOptions := ibmmq.MQOO_OUTPUT
	mqod.ObjectType = ibmmq.MQOT_Q
	mqod.ObjectName = sourceQueue

	qMgr = s.Connect()
	defer qMgr.Disc()
	qObject, err := qMgr.Open(mqod, openOptions)
	if err != nil {
		log.Fatal("Error in opening queue: " + err.Error())
	} else {
		defer qObject.Close(0)
	}
	putmqmd := ibmmq.NewMQMD()
	pmo := ibmmq.NewMQPMO()

	pmo.Options = ibmmq.MQPMO_NO_SYNCPOINT
	pmo.Options |= ibmmq.MQPMO_NEW_MSG_ID
	pmo.Options |= ibmmq.MQPMO_NEW_CORREL_ID
	pmo.Options |= ibmmq.MQPMO_FAIL_IF_QUIESCING

	putmqmd.Format = ibmmq.MQFMT_STRING
	putmqmd.ReplyToQ = replyQueue
	buffer := []byte(sourceMessage)

	// Put the message
	err = qObject.Put(putmqmd, pmo, buffer)

	if err != nil {
		log.Fatal("Error in putting msg: " + err.Error())
		msgId = ""
	} else {
		msgId = hex.EncodeToString(putmqmd.MsgId)
	}

	if simulateReply {
		s.replyToMessage(sourceQueue)
	}

	return msgId
}

func (s *Ibmmq) Receive(replyQueue string, msgId string, replyMsg string) int {
	var qMgr ibmmq.MQQueueManager
	var rc int

	mqod := ibmmq.NewMQOD()
	openOptions := ibmmq.MQOO_INPUT_SHARED
	mqod.ObjectType = ibmmq.MQOT_Q
	mqod.ObjectName = replyQueue
	qMgr = s.Connect()
	defer qMgr.Disc()

	qObject, err := qMgr.Open(mqod, openOptions)
	if err != nil {
		log.Fatal("Error in opening queue: " + err.Error())
	} else {
		defer qObject.Close(0)
	}

	getmqmd := ibmmq.NewMQMD()
	gmo := ibmmq.NewMQGMO()

	gmo.Options = ibmmq.MQGMO_NO_SYNCPOINT
	gmo.Options |= ibmmq.MQGMO_WAIT
	gmo.WaitInterval = 3 * 1000

	getmqmd.CorrelId, _ = hex.DecodeString(msgId)
	gmo.MatchOptions = ibmmq.MQMO_MATCH_CORREL_ID
	gmo.Version = ibmmq.MQGMO_VERSION_2

	buffer := make([]byte, 0, 1024)
	buffer, _, err = qObject.GetSlice(getmqmd, gmo, buffer)

	if err != nil {
		mqret := err.(*ibmmq.MQReturn)
		if mqret.MQRC == ibmmq.MQRC_NO_MSG_AVAILABLE {
			rc = 0
		}
		log.Fatal("Error getting message:" + err.Error())
		rc = 1
	} else {
		rc = 0
		if replyMsg != "" && replyMsg != string(buffer) {
			log.Fatal("Not the response we expect!")
			rc = 2
		}
	}
	return rc
}

/*
 * Simulate another application replying to a message.
 */
func (s *Ibmmq) replyToMessage(sendQueueName string) {
	var qMgr ibmmq.MQQueueManager

	mqod := ibmmq.NewMQOD()
	openOptions := ibmmq.MQOO_INPUT_SHARED
	mqod.ObjectType = ibmmq.MQOT_Q
	mqod.ObjectName = sendQueueName
	qMgr = s.Connect()
	defer qMgr.Disc()

	qObject, err := qMgr.Open(mqod, openOptions)
	if err != nil {
		log.Fatal("(SIM)Error in opening queue: " + err.Error())
	}

	getmqmd := ibmmq.NewMQMD()
	gmo := ibmmq.NewMQGMO()

	gmo.Options = ibmmq.MQGMO_NO_SYNCPOINT

	gmo.Options |= ibmmq.MQGMO_WAIT
	gmo.WaitInterval = 3 * 1000

	buffer := make([]byte, 0, 1024)
	buffer, _, err = qObject.GetSlice(getmqmd, gmo, buffer)
	qObject.Close(0)
	if err != nil {
		mqret := err.(*ibmmq.MQReturn)
		if mqret.MQRC != ibmmq.MQRC_NO_MSG_AVAILABLE {
			log.Fatal("(SIM)Error getting message:" + err.Error())
		}
	} else {
		mqod = ibmmq.NewMQOD()
		openOptions = ibmmq.MQOO_OUTPUT
		mqod.ObjectType = ibmmq.MQOT_Q
		mqod.ObjectName = getmqmd.ReplyToQ

		qObject, err = qMgr.Open(mqod, openOptions)
		if err != nil {
			log.Fatal("(SIM)Error in opening queue: " + err.Error())
		} else {
			defer qObject.Close(0)
		}

		putmqmd := ibmmq.NewMQMD()
		pmo := ibmmq.NewMQPMO()

		pmo.Options = ibmmq.MQPMO_NO_SYNCPOINT
		pmo.Options |= ibmmq.MQPMO_NEW_MSG_ID

		putmqmd.Format = ibmmq.MQFMT_STRING
		putmqmd.CorrelId = getmqmd.MsgId

		// Put the message
		err = qObject.Put(putmqmd, pmo, []byte("Reply Message"))

		if err != nil {
			log.Fatal("(SIM)Error in putting msg: " + err.Error())
		}
	}
}
