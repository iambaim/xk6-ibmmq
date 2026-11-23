package xk6ibmmq

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/ibm-messaging/mq-golang/v5/ibmmq"
	"go.k6.io/k6/v2/js/common"
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
	idleTimeout, err := connectionIdleTimeout(s.connectionEnv("MQ_CONNECTION_REUSE"), s.connectionEnv("MQ_CONNECTION_IDLE_TIMEOUT"))
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

// ConnectionReuseEnabled reports whether NewClient enabled connection reuse.
func (s *Ibmmq) ConnectionReuseEnabled() bool {
	return s.idleTimeout > 0
}

func (s *Ibmmq) connectionEnv(key string) string {
	if s.vu == nil {
		return os.Getenv(key)
	}
	rt := s.vu.Runtime()
	if env := rt.Get("__ENV"); !common.IsNullish(env) {
		if value := env.ToObject(rt).Get(key); !common.IsNullish(value) {
			return value.String()
		}
	}
	return ""
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

func (s *Ibmmq) withConnection(op func(*mqOperation) error) error {
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
	err := s.withConnection(func(op *mqOperation) error {
		var err error
		msgID, err = sendOn(op, sourceQueue, replyQueue, sourceMessage, extraProperties)
		return err
	})
	return msgID, err
}

func sendOn(op *mqOperation, sourceQueue, replyQueue string, sourceMessage any, extraProperties map[string]any) (string, error) {
	var putMsgHandle ibmmq.MQMessageHandle

	// Set queue open options
	mqod := ibmmq.NewMQOD()
	openOptions := ibmmq.MQOO_OUTPUT
	mqod.ObjectType = ibmmq.MQOT_Q
	mqod.ObjectName = sourceQueue

	// Open queue
	qObject, err := op.manager.Open(mqod, openOptions)
	if err != nil {
		return "", fmt.Errorf("error in opening queue: %w", err)
	}
	defer func() { op.recordCleanup("close output queue", qObject.Close(0)) }()

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
		putMsgHandle, err = op.manager.CrtMH(cmho)
		if err != nil {
			return "", fmt.Errorf("error in setting putMsgHandle: %w", err)
		}
		defer func() {
			op.recordCleanup("delete message handle", putMsgHandle.DltMH(ibmmq.NewMQDMHO()))
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

// Receive gets a message with a correlation ID matching msgId.
func (s *Ibmmq) Receive(replyQueue string, msgId string, waitInterval int32) (int, string, error) {
	correlationID, err := decodeCorrelationID(msgId)
	if err != nil {
		return 1, "", err
	}
	ctx := context.Background()
	if s.vu != nil && s.vu.Context() != nil {
		ctx = s.vu.Context()
	}
	if waitInterval < ibmmq.MQWI_UNLIMITED || (waitInterval == ibmmq.MQWI_UNLIMITED && ctx.Done() == nil) {
		return 1, "", fmt.Errorf("invalid wait interval %d: use milliseconds >= 0, or -1 with a cancellable VU context", waitInterval)
	}
	if err := ctx.Err(); err != nil {
		return 1, "", err
	}

	var reply string
	err = s.withConnection(func(op *mqOperation) error {
		var err error
		reply, err = receiveOn(ctx, op, replyQueue, correlationID, waitInterval)
		return err
	})
	if err != nil {
		return 1, "", err
	}
	return 0, reply, nil
}

func decodeCorrelationID(msgID string) ([]byte, error) {
	if len(msgID) != 2*int(ibmmq.MQ_CORREL_ID_LENGTH) {
		return nil, fmt.Errorf("invalid msgId: expected %d hexadecimal characters", 2*ibmmq.MQ_CORREL_ID_LENGTH)
	}
	id, err := hex.DecodeString(msgID)
	if err != nil {
		return nil, fmt.Errorf("invalid msgId: %w", err)
	}
	// MQCI_NONE would match any message on the reply queue.
	if bytes.Equal(id, make([]byte, ibmmq.MQ_CORREL_ID_LENGTH)) {
		return nil, fmt.Errorf("invalid msgId: an all-zero ID matches any reply in IBM MQ")
	}
	return id, nil
}

func receiveOn(ctx context.Context, op *mqOperation, replyQueue string, correlationID []byte, waitInterval int32) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Prepare to open queue
	mqod := ibmmq.NewMQOD()
	openOptions := ibmmq.MQOO_INPUT_SHARED
	mqod.ObjectType = ibmmq.MQOT_Q
	mqod.ObjectName = replyQueue

	// Open queue
	qObject, err := op.manager.Open(mqod, openOptions)
	if err != nil {
		return "", fmt.Errorf("error in opening queue: %w", err)
	}
	defer func() { op.recordCleanup("close reply queue", qObject.Close(0)) }()

	buffer := make([]byte, 0, 1048576)
	return waitForReply(ctx, waitInterval, func(wait int32) ([]byte, error) {
		// MQGET writes back to both structures. Set the match ID for each call.
		md := ibmmq.NewMQMD()
		copy(md.CorrelId, correlationID)
		gmo := ibmmq.NewMQGMO()
		gmo.Options = ibmmq.MQGMO_NO_SYNCPOINT | ibmmq.MQGMO_FAIL_IF_QUIESCING
		if wait > 0 {
			gmo.Options |= ibmmq.MQGMO_WAIT
		}
		gmo.WaitInterval = wait
		gmo.MatchOptions = ibmmq.MQMO_MATCH_CORREL_ID
		gmo.Version = ibmmq.MQGMO_VERSION_2
		data, _, err := qObject.GetSlice(md, gmo, buffer)
		return data, err
	})
}

// MQGET is synchronous. Bound each native wait so a canceled VU can stop
// waiting without disconnecting the handle from another goroutine.
func waitForReply(ctx context.Context, waitInterval int32, get func(int32) ([]byte, error)) (string, error) {
	const maxWait = 250 * time.Millisecond
	deadline := time.Now().Add(time.Duration(waitInterval) * time.Millisecond)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		wait := maxWait
		if waitInterval != ibmmq.MQWI_UNLIMITED {
			wait = min(wait, max(0, time.Until(deadline)))
		}
		if contextDeadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(contextDeadline)
			if remaining <= 0 {
				// The deadline can pass before the context's timer sets Err().
				return "", context.DeadlineExceeded
			}
			wait = min(wait, remaining)
		}
		// Round up sub-millisecond waits, but keep zero as a single no-wait get.
		data, err := get(int32((wait + time.Millisecond - 1) / time.Millisecond))
		if err == nil {
			return string(data), nil
		}
		if mqReason(err) != ibmmq.MQRC_NO_MSG_AVAILABLE {
			return "", fmt.Errorf("error getting message: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if waitInterval != ibmmq.MQWI_UNLIMITED && !time.Now().Before(deadline) {
			return "", nil
		}
	}
}

// getRequiredEnv returns the value of the environment variable or an error if not set.
func getRequiredEnv(key string) (string, error) {
	val := os.Getenv(key)
	if val == "" {
		return "", fmt.Errorf("required environment variable %s is not set", key)
	}
	return val, nil
}
