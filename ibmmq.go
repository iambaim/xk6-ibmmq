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

/*
 * Initialize Queue Manager connection.
 */
func (s *Ibmmq) NewClient() int {
	var rc int

	// Get all the environment variables
	QMName := env.MustGet("MQ_QMGR", env.String)
	Hostname := env.MustGet("MQ_HOST", env.String)
	PortNumber := env.MustGet("MQ_PORT", env.String)
	ChannelName := env.MustGet("MQ_CHANNEL", env.String)
	UserName := env.MustGet("MQ_USERID", env.String)
	Password := env.MustGet("MQ_PASSWORD", env.String)
	SSLKeystore := env.GetOr("MQ_TLS_KEYSTORE", env.String, "")
	SSLCipherSpec := env.GetOr("MQ_TLS_CIPHER_SPEC", env.String, "ANY_TLS12_OR_HIGHER")

	// Allocate new MQCNO and MQCD structures
	cno := ibmmq.NewMQCNO()
	cd := ibmmq.NewMQCD()

	// Setup channel and connection name
	cd.ChannelName = ChannelName
	cd.ConnectionName = Hostname + "(" + PortNumber + ")"

	// Set the connection paramters
	cno.ClientConn = cd
	cno.Options = ibmmq.MQCNO_CLIENT_BINDING
	cno.Options |= ibmmq.MQCNO_RECONNECT
	cno.Options |= ibmmq.MQCNO_HANDLE_SHARE_NO_BLOCK
	cno.Options |= ibmmq.MQCNO_ALL_CONVS_SHARE

	// Specify own name for the application
	cno.ApplName = "xk6-ibmmq"

	// If SSL is used set the necessary MQSCO
	if SSLKeystore != "" {
		sco := ibmmq.NewMQSCO()
		cd.SSLCipherSpec = SSLCipherSpec
		sco.KeyRepository = SSLKeystore
		cno.SSLConfig = sco
	}

	// If username is specified set MQCSP and filled the user and password variables
	if UserName != "" {
		csp := ibmmq.NewMQCSP()
		csp.AuthenticationType = ibmmq.MQCSP_AUTH_USER_ID_AND_PWD
		csp.UserId = UserName
		csp.Password = Password

		// Update the connection to use the auth info
		cno.SecurityParms = csp
	}

	// And now we can try to connect for the first time and defer the disconnection
	qMgr, err := ibmmq.Connx(QMName, cno)
	if err == nil {
		rc = 0
		defer qMgr.Disc()
		// Update the state information
		s.QMName = QMName
		s.cno = cno
	} else {
		rc = int(err.(*ibmmq.MQReturn).MQCC)
		log.Fatal("Error in making the initial connection: " + strconv.Itoa(rc) + err.Error())
	}
	return rc
}

/*
 * Connect to Queue Manager.
 */
func (s *Ibmmq) Connect() ibmmq.MQQueueManager {
	// Connect to the Queue Manager
	qMgr, err := ibmmq.Connx(s.QMName, s.cno)
	if err != nil {
		// if err.(*ibmmq.MQReturn).MQRC == ibmmq.MQRC_SSL_INITIALIZATION_ERROR {
		// 	for {
		// 		qMgr, err = ibmmq.Connx(s.QMName, s.cno)
		// 		if err == nil {
		// 			break
		// 		}
		// 	}
		// } else {
		rc := int(err.(*ibmmq.MQReturn).MQCC)
		log.Fatal("Error during Connect: " + strconv.Itoa(rc) + err.Error())
		// }
	}
	return qMgr
}

/*
 * Send a message into a sourceQueue, set reply queue == replyQueue, and return the Message ID.
 */
func (s *Ibmmq) Send(sourceQueue string, replyQueue string, sourceMessage any, extraProperties map[string]any, simulateReply bool) string {
	var msgId string
	var qMgr ibmmq.MQQueueManager
	var putMsgHandle ibmmq.MQMessageHandle

	// Set queue open options
	mqod := ibmmq.NewMQOD()
	openOptions := ibmmq.MQOO_OUTPUT | ibmmq.MQOO_INPUT_AS_Q_DEF
	mqod.ObjectType = ibmmq.MQOT_Q
	mqod.ObjectName = sourceQueue

	// Connect to Queue Manager
	qMgr = s.Connect()
	defer qMgr.Disc()

	// Open queue
	qObject, err := qMgr.Open(mqod, openOptions)
	if err != nil {
		log.Fatal("Error in opening queue: " + err.Error())
	} else {
		defer qObject.Close(0)
	}

	// Set new structures
	putmqmd := ibmmq.NewMQMD()
	pmo := ibmmq.NewMQPMO()

	// Set put options
	pmo.Options = ibmmq.MQPMO_NO_SYNCPOINT
	pmo.Options |= ibmmq.MQPMO_NEW_MSG_ID
	pmo.Options |= ibmmq.MQPMO_FAIL_IF_QUIESCING

	// Set reply queue
	putmqmd.ReplyToQ = replyQueue

	// Prepare the message data
	buffer := []byte{}
	if _, ok := sourceMessage.(string); ok {
		putmqmd.Format = ibmmq.MQFMT_STRING
		buffer = []byte(sourceMessage.(string))
	} else {
		putmqmd.Format = ibmmq.MQFMT_NONE
		buffer = sourceMessage.([]byte)
	}

	// Set extra properties
	if len(extraProperties) > 0 {
		cmho := ibmmq.NewMQCMHO()
		putMsgHandle, err = qMgr.CrtMH(cmho)
		if err != nil {
			log.Fatal("Error in setting putMsgHandle: " + err.Error())
		} else {
			defer dltMh(putMsgHandle)
		}

		smpo := ibmmq.NewMQSMPO()
		pd := ibmmq.NewMQPD()

		for k, v := range extraProperties {
			err = putMsgHandle.SetMP(smpo, k, pd, v)
			if err != nil {
				log.Fatal("Error in setting prop " + k + " : " + err.Error())
			}
		}

		pmo.OriginalMsgHandle = putMsgHandle
	}

	// Put the message
	err = qObject.Put(putmqmd, pmo, buffer)

	// Handle errors
	if err != nil {
		log.Fatal("Error in putting msg: " + err.Error())
		msgId = ""
	} else {
		msgId = hex.EncodeToString(putmqmd.MsgId)
	}

	// Check if we need to simulate the reply
	if simulateReply {
		s.replyToMessage(sourceQueue)
	}

	return msgId
}

/*
 * Receive a message, matching Correlation ID with the supplied msgId.
 */
func (s *Ibmmq) Receive(replyQueue string, msgId string, waitInterval int32) (int, string) {
	var qMgr ibmmq.MQQueueManager
	var rc int

	// Prepare to open queue
	mqod := ibmmq.NewMQOD()
	openOptions := ibmmq.MQOO_INPUT_SHARED
	mqod.ObjectType = ibmmq.MQOT_Q
	mqod.ObjectName = replyQueue

	// Call connect
	qMgr = s.Connect()
	defer qMgr.Disc()

	// Open queue
	qObject, err := qMgr.Open(mqod, openOptions)
	if err != nil {
		log.Fatal("Error in opening queue: " + err.Error())
	} else {
		defer qObject.Close(0)
	}

	// Prepare new structures
	getmqmd := ibmmq.NewMQMD()
	gmo := ibmmq.NewMQGMO()

	// Wait for a while for the message to arrive
	gmo.Options = ibmmq.MQGMO_NO_SYNCPOINT
	gmo.Options |= ibmmq.MQGMO_WAIT
	gmo.WaitInterval = waitInterval

	// Match the correlation id
	getmqmd.CorrelId, _ = hex.DecodeString(msgId)
	gmo.MatchOptions = ibmmq.MQMO_MATCH_CORREL_ID
	gmo.Version = ibmmq.MQGMO_VERSION_2

	// Get message
	buffer := make([]byte, 0, 1048576)
	buffer, _, err = qObject.GetSlice(getmqmd, gmo, buffer)

	// Handle errors
	if err != nil {
		mqret := err.(*ibmmq.MQReturn)
		if mqret.MQRC == ibmmq.MQRC_NO_MSG_AVAILABLE {
			rc = 0
		}
		log.Fatal("Error getting message:" + err.Error())
		rc = 1
	} else {
		rc = 0
	}
	return rc, string(buffer)
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
		openOptions = ibmmq.MQOO_OUTPUT | ibmmq.MQOO_INPUT_AS_Q_DEF
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

		err = qObject.Put(putmqmd, pmo, []byte("Reply Message"))

		if err != nil {
			log.Fatal("(SIM)Error in putting msg: " + err.Error())
		}
	}
}

// Clean up message handle
func dltMh(mh ibmmq.MQMessageHandle) error {
	dmho := ibmmq.NewMQDMHO()
	err := mh.DltMH(dmho)
	if err != nil {
		log.Fatal("Unable to close a msg handle!")
	}
	return err
}
