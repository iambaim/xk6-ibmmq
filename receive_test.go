package xk6ibmmq

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ibm-messaging/mq-golang/v5/ibmmq"
)

func TestReceiveRejectsInvalidCorrelationIDBeforeConnecting(t *testing.T) {
	// An unconfigured client would return a connection error if validation ran late.
	var client Ibmmq
	for _, id := range []string{"", "01", strings.Repeat("00", 24), strings.Repeat("zz", 24), strings.Repeat("AB", 25)} {
		rc, reply, err := client.Receive("REPLY", id, 0)
		if rc != 1 || reply != "" || err == nil || !strings.Contains(err.Error(), "invalid msgId") {
			t.Errorf("Receive with %q returned %d, %q, %v", id, rc, reply, err)
		}
	}
	if id, err := decodeCorrelationID(strings.Repeat("Ab", 24)); err != nil || !slices.Equal(id, slices.Repeat([]byte{0xab}, 24)) {
		t.Fatalf("expected mixed-case hex to decode, got %x, %v", id, err)
	}
}

func TestReceiveRejectsInvalidWaitBeforeConnecting(t *testing.T) {
	var client Ibmmq
	for _, wait := range []int32{-2, ibmmq.MQWI_UNLIMITED} {
		_, _, err := client.Receive("REPLY", strings.Repeat("AB", 24), wait)
		if err == nil || !strings.Contains(err.Error(), "invalid wait interval") {
			t.Errorf("Receive with wait %d: got %v", wait, err)
		}
	}
}

func TestWaitForReplyHonorsTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, test := range []struct {
			wait  int32
			calls []int32
		}{
			{0, []int32{0}},
			{100, []int32{100}},
			{600, []int32{250, 250, 100}},
		} {
			var calls []int32
			reply, err := waitForReply(t.Context(), test.wait, func(wait int32) ([]byte, error) {
				calls = append(calls, wait)
				time.Sleep(time.Duration(wait) * time.Millisecond)
				return nil, &ibmmq.MQReturn{MQRC: ibmmq.MQRC_NO_MSG_AVAILABLE}
			})
			if reply != "" || err != nil || !slices.Equal(calls, test.calls) {
				t.Errorf("wait %d: got reply %q, error %v, calls %v; want calls %v", test.wait, reply, err, calls, test.calls)
			}
		}
	})
}

func TestWaitForReplyStopsOnCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		time.AfterFunc(10*time.Millisecond, cancel)
		calls := 0
		reply, err := waitForReply(ctx, ibmmq.MQWI_UNLIMITED, func(wait int32) ([]byte, error) {
			calls++
			time.Sleep(time.Duration(wait) * time.Millisecond)
			return nil, &ibmmq.MQReturn{MQRC: ibmmq.MQRC_NO_MSG_AVAILABLE}
		})
		if reply != "" || !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatalf("expected cancellation after one bounded wait, got %q, %v, calls=%d", reply, err, calls)
		}
	})
}

func TestWaitForReplyUsesContextDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		gotWait := int32(-1)
		_, err := waitForReply(ctx, 3000, func(wait int32) ([]byte, error) {
			gotWait = wait
			time.Sleep(time.Duration(wait) * time.Millisecond)
			return nil, &ibmmq.MQReturn{MQRC: ibmmq.MQRC_NO_MSG_AVAILABLE}
		})
		if gotWait != 50 || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context deadline after 50ms, got wait %d, %v", gotWait, err)
		}
	})
}

func TestWaitForReplyDoesNotRetryOtherMQErrors(t *testing.T) {
	want := &ibmmq.MQReturn{MQRC: ibmmq.MQRC_CONNECTION_BROKEN}
	calls := 0
	_, err := waitForReply(t.Context(), 3000, func(int32) ([]byte, error) {
		calls++
		return nil, want
	})
	if calls != 1 || !errors.Is(err, want) {
		t.Fatalf("expected one call and the original MQ error, got calls=%d, %v", calls, err)
	}
}
