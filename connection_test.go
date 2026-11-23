package xk6ibmmq

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ibm-messaging/mq-golang/v5/ibmmq"
)

type connectionFixture struct {
	dials  atomic.Int32
	closes atomic.Int32
}

func (f *connectionFixture) dial() (*mqConnection, error) {
	f.dials.Add(1)
	return &mqConnection{
		manager: new(ibmmq.MQQueueManager),
		close: func() error {
			f.closes.Add(1)
			return nil
		},
	}, nil
}

func noop(*mqOperation) error { return nil }

func TestConnectionsAreIsolated(t *testing.T) {
	var a, b connectionCache
	var first, second connectionFixture
	t.Cleanup(func() { _ = a.close(); _ = b.close() })
	var firstHandle, secondHandle *ibmmq.MQQueueManager
	remember := func(handle **ibmmq.MQQueueManager) func(*mqOperation) error {
		return func(op *mqOperation) error {
			conn := op.manager
			if *handle != nil && *handle != conn {
				return errors.New("connection unexpectedly replaced")
			}
			*handle = conn
			return nil
		}
	}
	for range 5 {
		if err := a.use(time.Minute, first.dial, remember(&firstHandle)); err != nil {
			t.Fatal(err)
		}
		if err := b.use(time.Minute, second.dial, remember(&secondHandle)); err != nil {
			t.Fatal(err)
		}
	}
	if firstHandle == secondHandle || first.dials.Load() != 1 || second.dials.Load() != 1 {
		t.Fatal("expected each VU to reuse its own handle")
	}
	for range 2 {
		if err := a.close(); err != nil {
			t.Fatal(err)
		}
	}
	if first.closes.Load() != 1 || second.closes.Load() != 0 {
		t.Fatal("close must be idempotent and affect only its VU")
	}
	if err := a.use(time.Minute, first.dial, noop); err != nil || first.dials.Load() != 2 {
		t.Fatalf("expected an explicit close to allow reopening, got %v", err)
	}
	if err := b.use(time.Minute, second.dial, remember(&secondHandle)); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionFailureDoesNotReplayOperation(t *testing.T) {
	var cache connectionCache
	var f connectionFixture
	t.Cleanup(func() { _ = cache.close() })

	for _, reason := range []int32{ibmmq.MQRC_NO_MSG_AVAILABLE, ibmmq.MQRC_UNKNOWN_OBJECT_NAME} {
		want := fmt.Errorf("operation: %w", &ibmmq.MQReturn{MQRC: reason})
		if got := cache.use(time.Minute, f.dial, func(*mqOperation) error { return want }); !errors.Is(got, want) {
			t.Fatalf("expected original MQ error, got %v", got)
		}
	}
	if f.dials.Load() != 1 || f.closes.Load() != 0 {
		t.Fatal("timeout and queue errors must not invalidate the connection")
	}

	want := fmt.Errorf("put: %w", &ibmmq.MQReturn{MQRC: ibmmq.MQRC_CONNECTION_BROKEN})
	calls := 0
	if got := cache.use(time.Minute, f.dial, func(*mqOperation) error {
		calls++
		return want
	}); !errors.Is(got, want) {
		t.Fatalf("expected original connection error, got %v", got)
	}
	if calls != 1 || f.dials.Load() != 1 || f.closes.Load() != 1 {
		t.Fatal("failed operation should run once and invalidate the connection")
	}
	if err := cache.use(time.Minute, f.dial, noop); err != nil || f.dials.Load() != 2 {
		t.Fatalf("next operation should open a new connection, got %v", err)
	}
}

func TestCleanupFailureRetiresConnectionWithoutChangingResult(t *testing.T) {
	for _, original := range []error{nil, errors.New("put failed")} {
		var cache connectionCache
		var f connectionFixture
		if got := cache.use(time.Minute, f.dial, func(op *mqOperation) error {
			op.recordCleanup("close output queue", errors.New("close failed"))
			op.recordCleanup("delete message handle", nil)
			return original
		}); got != original {
			t.Fatalf("cleanup changed the operation result: got %v, want %v", got, original)
		}
		if f.dials.Load() != 1 || f.closes.Load() != 1 {
			t.Fatal("expected cleanup failure to discard the cached connection")
		}
		if err := cache.use(time.Minute, f.dial, noop); err != nil || f.dials.Load() != 2 {
			t.Fatalf("expected a fresh connection, got %v", err)
		}
		_ = cache.close()
	}
}

func TestUncachedConnectionsAreClosedAfterEachOperation(t *testing.T) {
	var cache connectionCache
	var f connectionFixture
	for range 3 {
		if err := cache.use(0, f.dial, noop); err != nil {
			t.Fatal(err)
		}
	}
	if f.dials.Load() != 3 || f.closes.Load() != 3 {
		t.Fatal("expected one dial and close per operation")
	}
}

func TestIdleTimerCannotCloseAnActiveOperation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cache connectionCache
		var f connectionFixture
		t.Cleanup(func() { _ = cache.close() })
		const idle = time.Second
		if err := cache.use(idle, f.dial, noop); err != nil {
			t.Fatal(err)
		}
		if err := cache.use(idle, f.dial, func(*mqOperation) error {
			time.Sleep(2 * idle)
			if f.closes.Load() != 0 {
				return errors.New("connection closed during operation")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(idle / 2)
		synctest.Wait()
		if f.closes.Load() != 0 {
			t.Fatal("idle timeout must start after the operation returns")
		}
		time.Sleep(idle)
		synctest.Wait()
		if f.closes.Load() != 1 {
			t.Fatal("idle connection was not closed")
		}
		if err := cache.use(idle, f.dial, noop); err != nil || f.dials.Load() != 2 {
			t.Fatalf("expected to redial after idle disconnect, got %v", err)
		}
	})
}

func TestFailedDialCanBeRetried(t *testing.T) {
	var cache connectionCache
	var fixture connectionFixture
	t.Cleanup(func() { _ = cache.close() })
	want := errors.New("dial failed")
	opCalls := 0
	op := func(*mqOperation) error {
		opCalls++
		return nil
	}
	if err := cache.use(time.Minute, func() (*mqConnection, error) { return nil, want }, op); !errors.Is(err, want) {
		t.Fatalf("expected dial error, got %v", err)
	}
	if opCalls != 0 {
		t.Fatal("must not run the operation when dial fails")
	}
	if err := cache.use(time.Minute, fixture.dial, op); err != nil || opCalls != 1 {
		t.Fatalf("expected next operation to dial and run, got %v", err)
	}
}

func TestConnectionIdleTimeoutConfiguration(t *testing.T) {
	for _, tc := range []struct {
		reuse, timeout string
		want           time.Duration
		invalid        bool
	}{
		{"", "", 0, false},
		{"false", "", 0, false},
		{"true", "", 30 * time.Second, false},
		{"true", "500ms", 500 * time.Millisecond, false},
		{"true", "0s", 0, true},
		{"true", "-1s", 0, true},
		{"true", "invalid", 0, true},
		{"enabled", "", 0, true},
	} {
		got, err := connectionIdleTimeout(tc.reuse, tc.timeout)
		if got != tc.want || (err != nil) != tc.invalid {
			t.Errorf("connectionIdleTimeout(%q, %q) = %v, %v", tc.reuse, tc.timeout, got, err)
		}
	}
}
