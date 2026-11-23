package xk6ibmmq

import (
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/ibm-messaging/mq-golang/v5/ibmmq"
	"go.k6.io/k6/v2/js/modules"
	"go.k6.io/k6/v2/metrics"
)

func init() {
	modules.Register("k6/x/ibmmq", new(RootModule))
}

// RootModule is the global module object registered with k6.
// It creates a new per-VU ModuleInstance for each VU.
type RootModule struct{}

// Ensure RootModule implements modules.Module.
var _ modules.Module = &RootModule{}

// NewModuleInstance creates a new per-VU module instance.
func (*RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	client := &Ibmmq{vu: vu}
	if env := vu.InitEnv(); env != nil && env.TestPreInitState != nil && env.Registry != nil {
		client.connectionMetric = env.Registry.MustNewMetric("ibmmq_connections", metrics.Counter)
	}
	return &ModuleInstance{ibmmq: client}
}

// ModuleInstance is a per-VU instance of the IBM MQ module.
type ModuleInstance struct {
	ibmmq *Ibmmq
}

// Ensure ModuleInstance implements modules.Instance.
var _ modules.Instance = &ModuleInstance{}

// Exports returns the module's exported members accessible from JavaScript.
func (mi *ModuleInstance) Exports() modules.Exports {
	return modules.Exports{
		Default: mi.ibmmq,
	}
}

// Ibmmq is the per-VU IBM MQ client exposed to JavaScript.
type Ibmmq struct {
	QMName           string
	cno              *ibmmq.MQCNO
	vu               modules.VU
	connectionMetric *metrics.Metric
	idleTimeout      time.Duration
	connections      connectionCache
}

// NewClient configures the queue manager without opening a connection.
func (s *Ibmmq) NewClient() (int, error) {
	// Get all the environment variables
	QMName, err := getRequiredEnv("MQ_QMGR")
	if err != nil {
		return 0, err
	}
	Hostname, err := getRequiredEnv("MQ_HOST")
	if err != nil {
		return 0, err
	}
	PortNumber, err := getRequiredEnv("MQ_PORT")
	if err != nil {
		return 0, err
	}
	ChannelName, err := getRequiredEnv("MQ_CHANNEL")
	if err != nil {
		return 0, err
	}
	UserName := os.Getenv("MQ_USERID")
	Password := os.Getenv("MQ_PASSWORD")
	SSLKeystore := os.Getenv("MQ_TLS_KEYSTORE")
	SSLCipherSpec := os.Getenv("MQ_TLS_CIPHER_SPEC")
	if SSLCipherSpec == "" {
		SSLCipherSpec = "ANY_TLS12_OR_HIGHER"
	}
	idleTimeout, err := connectionIdleTimeout(os.Getenv("MQ_CONNECTION_REUSE"), os.Getenv("MQ_CONNECTION_IDLE_TIMEOUT"))
	if err != nil {
		return 0, err
	}

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

	// Store the state information for later use by Connect()
	if err := s.Close(); err != nil {
		return 0, err
	}
	s.QMName = QMName
	s.cno = cno
	s.idleTimeout = idleTimeout
	return 0, nil
}

// Connect opens an uncached connection. Its caller owns the handle.
func (s *Ibmmq) Connect() (ibmmq.MQQueueManager, error) {
	return s.connect(false)
}

func (s *Ibmmq) connect(reuse bool) (ibmmq.MQQueueManager, error) {
	if s.cno == nil {
		return ibmmq.MQQueueManager{}, fmt.Errorf("error during Connect: client not initialized, call NewClient() first")
	}
	options := *s.cno
	if reuse {
		// Reopen on a later call instead of automatically reconnecting in this call.
		options.Options &^= ibmmq.MQCNO_RECONNECT
		options.Options |= ibmmq.MQCNO_RECONNECT_DISABLED
	}
	qMgr, err := ibmmq.Connx(s.QMName, &options)
	if err != nil {
		return qMgr, fmt.Errorf("error during Connect: %w", err)
	}
	return qMgr, nil
}

// Close disconnects this VU's cached connection. The next call can reopen it.
func (s *Ibmmq) Close() error {
	if err := s.connections.close(); err != nil && mqReason(err) != ibmmq.MQRC_HCONN_ERROR {
		return fmt.Errorf("error disconnecting from IBM MQ: %w", err)
	}
	return nil
}

func connectionIdleTimeout(reuse, timeout string) (time.Duration, error) {
	if reuse == "" {
		return 0, nil
	}
	enabled, err := strconv.ParseBool(reuse)
	if err != nil {
		return 0, fmt.Errorf("MQ_CONNECTION_REUSE must be a boolean: %w", err)
	}
	if !enabled {
		return 0, nil
	}
	if timeout == "" {
		return 30 * time.Second, nil
	}
	duration, err := time.ParseDuration(timeout)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("MQ_CONNECTION_IDLE_TIMEOUT must be a positive duration, for example 30s")
	}
	return duration, nil
}

func (s *Ibmmq) withConnection(op func(*ibmmq.MQQueueManager) error) error {
	var idle time.Duration
	if s.vu != nil {
		// Setup and teardown use a temporary VU with ID 0. Keep their
		// connections short-lived, and do not connect from the init context in reuse mode.
		state := s.vu.State()
		if state == nil && s.idleTimeout > 0 {
			return fmt.Errorf("IBM MQ send and receive are not available in the init context with connection reuse enabled")
		}
		if state != nil && state.VUID != 0 {
			idle = s.idleTimeout
		}
	}
	return s.connections.use(idle, func() (*mqConnection, error) {
		manager, err := s.connect(idle > 0)
		if err != nil {
			return nil, err
		}
		if s.connectionMetric != nil {
			if state := s.vu.State(); state != nil && state.VUID != 0 {
				tags := state.Tags.GetCurrentValues()
				metrics.PushIfNotDone(s.vu.Context(), state.Samples, metrics.Sample{
					TimeSeries: metrics.TimeSeries{Metric: s.connectionMetric, Tags: tags.Tags},
					Time:       time.Now(),
					Value:      1,
					Metadata:   tags.Metadata,
				})
			}
		}
		return &mqConnection{manager: &manager, close: manager.Disc}, nil
	}, op)
}

// Send puts a message on sourceQueue and returns its message ID. If replyQueue
// is set, the message is an MQ request; a separate consumer sends the reply.
func (s *Ibmmq) Send(sourceQueue string, replyQueue string, sourceMessage any, extraProperties map[string]any) (string, error) {
	var msgID string
	err := s.withConnection(func(manager *ibmmq.MQQueueManager) error {
		var err error
		msgID, err = sendOn(manager, sourceQueue, replyQueue, sourceMessage, extraProperties)
		return err
	})
	return msgID, err
}

func sendOn(qMgr *ibmmq.MQQueueManager, sourceQueue, replyQueue string, sourceMessage any, extraProperties map[string]any) (string, error) {
	var putMsgHandle ibmmq.MQMessageHandle

	// Set queue open options
	mqod := ibmmq.NewMQOD()
	openOptions := ibmmq.MQOO_OUTPUT
	mqod.ObjectType = ibmmq.MQOT_Q
	mqod.ObjectName = sourceQueue

	// Open queue
	qObject, err := qMgr.Open(mqod, openOptions)
	if err != nil {
		return "", fmt.Errorf("error in opening queue: %w", err)
	}
	defer qObject.Close(0)

	// Set new structures
	putmqmd := ibmmq.NewMQMD()
	pmo := ibmmq.NewMQPMO()

	// Set put options
	pmo.Options = ibmmq.MQPMO_NO_SYNCPOINT
	pmo.Options |= ibmmq.MQPMO_NEW_MSG_ID
	pmo.Options |= ibmmq.MQPMO_FAIL_IF_QUIESCING

	if replyQueue != "" {
		putmqmd.MsgType = ibmmq.MQMT_REQUEST
		putmqmd.ReplyToQ = replyQueue
	}

	// Prepare the message data
	var buffer []byte
	switch msg := sourceMessage.(type) {
	case string:
		putmqmd.Format = ibmmq.MQFMT_STRING
		buffer = []byte(msg)
	case []byte:
		putmqmd.Format = ibmmq.MQFMT_NONE
		buffer = msg
	default:
		return "", fmt.Errorf("unsupported sourceMessage type %T, expected string or []byte", sourceMessage)
	}

	// Set extra properties
	if len(extraProperties) > 0 {
		cmho := ibmmq.NewMQCMHO()
		putMsgHandle, err = qMgr.CrtMH(cmho)
		if err != nil {
			return "", fmt.Errorf("error in setting putMsgHandle: %w", err)
		}
		defer func() {
			if err := dltMh(putMsgHandle); err != nil {
				log.Printf("warning: %v", err)
			}
		}()

		smpo := ibmmq.NewMQSMPO()
		pd := ibmmq.NewMQPD()

		for k, v := range extraProperties {
			err = putMsgHandle.SetMP(smpo, k, pd, v)
			if err != nil {
				return "", fmt.Errorf("error in setting prop %s: %w", k, err)
			}
		}

		pmo.OriginalMsgHandle = putMsgHandle
	}

	if err := qObject.Put(putmqmd, pmo, buffer); err != nil {
		return "", fmt.Errorf("error in putting msg: %w", err)
	}
	return hex.EncodeToString(putmqmd.MsgId), nil
}

/*
 * Receive a message, matching Correlation ID with the supplied msgId.
 */
func (s *Ibmmq) Receive(replyQueue string, msgId string, waitInterval int32) (int, string, error) {
	var reply string
	err := s.withConnection(func(manager *ibmmq.MQQueueManager) error {
		var err error
		reply, err = receiveOn(manager, replyQueue, msgId, waitInterval)
		return err
	})
	if err != nil {
		return 1, "", err
	}
	return 0, reply, nil
}

func receiveOn(qMgr *ibmmq.MQQueueManager, replyQueue, msgId string, waitInterval int32) (string, error) {
	// Prepare to open queue
	mqod := ibmmq.NewMQOD()
	openOptions := ibmmq.MQOO_INPUT_SHARED
	mqod.ObjectType = ibmmq.MQOT_Q
	mqod.ObjectName = replyQueue

	// Open queue
	qObject, err := qMgr.Open(mqod, openOptions)
	if err != nil {
		return "", fmt.Errorf("error in opening queue: %w", err)
	}
	defer qObject.Close(0)

	// Prepare new structures
	getmqmd := ibmmq.NewMQMD()
	gmo := ibmmq.NewMQGMO()

	// Wait for a while for the message to arrive
	gmo.Options = ibmmq.MQGMO_NO_SYNCPOINT
	gmo.Options |= ibmmq.MQGMO_WAIT
	gmo.WaitInterval = waitInterval

	// Match the correlation id
	getmqmd.CorrelId, err = hex.DecodeString(msgId)
	if err != nil {
		return "", fmt.Errorf("invalid msgId hex string %q: %w", msgId, err)
	}
	gmo.MatchOptions = ibmmq.MQMO_MATCH_CORREL_ID
	gmo.Version = ibmmq.MQGMO_VERSION_2

	// Get message
	buffer := make([]byte, 0, 1048576)
	buffer, _, err = qObject.GetSlice(getmqmd, gmo, buffer)

	// Handle errors
	if err != nil {
		if mqReason(err) == ibmmq.MQRC_NO_MSG_AVAILABLE {
			return "", nil
		}
		return "", fmt.Errorf("error getting message: %w", err)
	}
	return string(buffer), nil
}

// Clean up message handle
func dltMh(mh ibmmq.MQMessageHandle) error {
	dmho := ibmmq.NewMQDMHO()
	err := mh.DltMH(dmho)
	if err != nil {
		return fmt.Errorf("unable to close a msg handle: %w", err)
	}
	return nil
}

// getRequiredEnv returns the value of the environment variable or an error if not set.
func getRequiredEnv(key string) (string, error) {
	val := os.Getenv(key)
	if val == "" {
		return "", fmt.Errorf("required environment variable %s is not set", key)
	}
	return val, nil
}
