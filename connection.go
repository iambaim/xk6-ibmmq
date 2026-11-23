package xk6ibmmq

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ibm-messaging/mq-golang/v5/ibmmq"
)

type mqConnection struct {
	manager *ibmmq.MQQueueManager
	close   func() error
}

type mqOperation struct {
	manager    *ibmmq.MQQueueManager
	cleanupErr error
}

func (op *mqOperation) recordCleanup(action string, err error) {
	if err != nil {
		op.cleanupErr = errors.Join(op.cleanupErr, fmt.Errorf("%s: %w", action, err))
	}
}

func (op *mqOperation) logCleanupError() {
	if op.cleanupErr != nil {
		log.Printf("IBM MQ handle cleanup: %v", op.cleanupErr)
	}
}

// connectionCache belongs to one module instance. Keep its lock during MQ calls
// so an idle timer or an explicit close cannot disconnect an in-flight call.
type connectionCache struct {
	mu    sync.Mutex
	conn  *mqConnection
	timer *time.Timer
}

func (c *connectionCache) use(idle time.Duration, dial func() (*mqConnection, error), run func(*mqOperation) error) error {
	if idle == 0 {
		conn, err := dial()
		if err != nil {
			return err
		}
		defer func() { logDisconnectError(conn.close()) }()
		op := &mqOperation{manager: conn.manager}
		defer op.logCleanupError()
		return run(op)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if c.conn == nil {
		conn, err := dial()
		if err != nil {
			return err
		}
		c.conn = conn
	}

	op := &mqOperation{manager: c.conn.manager}
	var opErr error
	defer func() {
		op.logCleanupError()
		// A cleanup error should retire the connection without changing the
		// result of a put or get that already completed.
		if op.cleanupErr != nil || connectionLost(opErr) {
			logDisconnectError(c.closeLocked())
			return
		}
		c.scheduleIdleClose(idle)
	}()
	// Run the operation once. A failed put may already have reached MQ.
	opErr = run(op)
	return opErr
}

// scheduleIdleClose requires c.mu.
func (c *connectionCache) scheduleIdleClose(idle time.Duration) {
	var timer *time.Timer
	timer = time.AfterFunc(idle, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		// A newer operation may have replaced this timer before it got the lock.
		if c.timer == timer {
			logDisconnectError(c.closeLocked())
		}
	})
	c.timer = timer
}

func (c *connectionCache) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeLocked()
}

func (c *connectionCache) closeLocked() error {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	conn := c.conn
	c.conn = nil
	if conn == nil {
		return nil
	}
	return conn.close()
}

func mqReason(err error) int32 {
	var mqErr *ibmmq.MQReturn
	if errors.As(err, &mqErr) {
		return mqErr.MQRC
	}
	return ibmmq.MQRC_NONE
}

func connectionLost(err error) bool {
	switch mqReason(err) {
	case ibmmq.MQRC_CONNECTION_BROKEN, ibmmq.MQRC_HCONN_ERROR,
		ibmmq.MQRC_CONNECTION_QUIESCING, ibmmq.MQRC_CONNECTION_STOPPING, ibmmq.MQRC_CONNECTION_STOPPED,
		ibmmq.MQRC_CHANNEL_STOPPED, ibmmq.MQRC_CHANNEL_STOPPED_BY_USER,
		ibmmq.MQRC_Q_MGR_NOT_AVAILABLE, ibmmq.MQRC_Q_MGR_QUIESCING, ibmmq.MQRC_Q_MGR_STOPPING,
		ibmmq.MQRC_RECONNECT_FAILED, ibmmq.MQRC_CALL_INTERRUPTED:
		return true
	default:
		return false
	}
}

func logDisconnectError(err error) {
	if err != nil && mqReason(err) != ibmmq.MQRC_HCONN_ERROR {
		log.Printf("IBM MQ disconnect: %v", err)
	}
}
