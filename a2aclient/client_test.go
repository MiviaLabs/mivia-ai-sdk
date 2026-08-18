package a2aclient

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// expiresAfterCtx wraps context.Background and reports itself expired
// only once its Err method has been called more than callsBeforeExpiry
// times. It lets a test simulate a deadline that expires between two
// calls, deterministically and without a real sleep.
type expiresAfterCtx struct {
	context.Context
	callsBeforeExpiry int32
	calls             atomic.Int32
}

func (c *expiresAfterCtx) Err() error {
	if c.calls.Add(1) > c.callsBeforeExpiry {
		return context.DeadlineExceeded
	}
	return nil
}

func TestNewRejectsEmptyBaseURL(t *testing.T) {
	c, err := New("")
	if err == nil {
		t.Fatal("New(\"\") returned nil error")
	}
	if !errors.Is(err, ErrNoBaseURL) {
		t.Fatalf("New(\"\") error = %v, want errors.Is ErrNoBaseURL", err)
	}
	if c != nil {
		t.Fatal("New(\"\") returned a non-nil Client on error")
	}
}

func TestNewOpensLazyTransportWithoutDialing(t *testing.T) {
	// grpc.NewClient does not connect synchronously, so New succeeds
	// against an address nothing is listening on.
	c, err := New("dns:///agent.example.invalid:443")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c == nil {
		t.Fatal("New returned a nil Client on success")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestNewRejectsMalformedBaseURL(t *testing.T) {
	// A NUL byte fails gRPC's target URL parse synchronously, so New
	// returns an error without a live network call.
	c, err := New("\x00")
	if err == nil {
		t.Fatal("New accepted a malformed baseURL")
	}
	if c != nil {
		t.Fatal("New returned a non-nil Client on error")
	}
}

func TestNewFromTransportRejectsEmptyBaseURL(t *testing.T) {
	c, err := newFromTransport("", &stubTransport{})
	if err == nil {
		t.Fatal("newFromTransport(\"\", ...) returned nil error")
	}
	if !errors.Is(err, ErrNoBaseURL) {
		t.Fatalf("newFromTransport(\"\", ...) error = %v, want errors.Is ErrNoBaseURL", err)
	}
	if c != nil {
		t.Fatal("newFromTransport(\"\", ...) returned a non-nil Client on error")
	}
}

func TestNewFromTransportRejectsNilTransport(t *testing.T) {
	c, err := newFromTransport(testBaseURL, nil)
	if err == nil {
		t.Fatal("newFromTransport(..., nil) returned nil error")
	}
	if !errors.Is(err, ErrNoTransport) {
		t.Fatalf("newFromTransport(..., nil) error = %v, want errors.Is ErrNoTransport", err)
	}
	if c != nil {
		t.Fatal("newFromTransport(..., nil) returned a non-nil Client on error")
	}
}

func TestSendReturnsNonZeroTaskHandle(t *testing.T) {
	tr := &stubTransport{taskID: "task-1"}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	h, err := c.Send(context.Background(), signedMessage(t))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if h == (TaskHandle{}) {
		t.Fatal("Send returned a zero TaskHandle")
	}
}

func TestSendRejectsInvalidMessage(t *testing.T) {
	tr := &stubTransport{taskID: "task-1"}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	// Missing ThreadID fails envelope.Validate, which a2a.ToPart calls.
	invalid := envelope.Message{Version: envelope.Version, ID: "msg-1"}
	h, err := c.Send(context.Background(), invalid)
	if err == nil {
		t.Fatal("Send accepted an invalid message")
	}
	if h != (TaskHandle{}) {
		t.Fatal("Send returned a non-zero TaskHandle on error")
	}
}

func TestSendPropagatesTransportFailure(t *testing.T) {
	tr := &stubTransport{sendErr: errors.New("connection refused")}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	h, err := c.Send(context.Background(), signedMessage(t))
	if err == nil {
		t.Fatal("Send accepted a transport failure")
	}
	if h != (TaskHandle{}) {
		t.Fatal("Send returned a non-zero TaskHandle on transport failure")
	}
}

func TestStatusReturnsEachStateValue(t *testing.T) {
	cases := []State{
		StateSubmitted,
		StateWorking,
		StateCompleted,
		StateFailed,
		StateCanceled,
	}
	for _, want := range cases {
		t.Run(want.String(), func(t *testing.T) {
			tr := &stubTransport{taskID: "task-1", states: []State{want}}
			c, err := newFromTransport(testBaseURL, tr)
			if err != nil {
				t.Fatalf("newFromTransport: %v", err)
			}
			h, err := c.Send(context.Background(), signedMessage(t))
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			got, err := c.Status(context.Background(), h)
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if got != want {
				t.Fatalf("Status = %s, want %s", got, want)
			}
		})
	}
}

func TestStateStringCoversUnspecifiedAndUnknown(t *testing.T) {
	if got := StateUnspecified.String(); got != "unspecified" {
		t.Fatalf("StateUnspecified.String() = %q, want unspecified", got)
	}
	if got := State(99).String(); got != "unknown" {
		t.Fatalf("State(99).String() = %q, want unknown", got)
	}
}

func TestStatusRejectsZeroTaskHandle(t *testing.T) {
	tr := &stubTransport{taskID: "task-1"}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	if _, err := c.Status(context.Background(), TaskHandle{}); err == nil {
		t.Fatal("Status accepted the zero TaskHandle")
	} else if !errors.Is(err, ErrZeroTaskHandle) {
		t.Fatalf("Status error = %v, want errors.Is ErrZeroTaskHandle", err)
	}
}

func TestResultRejectsZeroTaskHandle(t *testing.T) {
	tr := &stubTransport{taskID: "task-1"}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	if _, err := c.Result(context.Background(), TaskHandle{}); err == nil {
		t.Fatal("Result accepted the zero TaskHandle")
	} else if !errors.Is(err, ErrZeroTaskHandle) {
		t.Fatalf("Result error = %v, want errors.Is ErrZeroTaskHandle", err)
	}
}

func TestResultRejectsNonTerminalState(t *testing.T) {
	tr := &stubTransport{taskID: "task-1", states: []State{StateWorking}}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	h, err := c.Send(context.Background(), signedMessage(t))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := c.Result(context.Background(), h); err == nil {
		t.Fatal("Result accepted a non-terminal task")
	} else if !errors.Is(err, ErrNotTerminal) {
		t.Fatalf("Result error = %v, want errors.Is ErrNotTerminal", err)
	}
}

func TestResultRejectsTamperedSignature(t *testing.T) {
	msg := signedMessage(t)
	mapped := mappedResult(t, msg)
	// Tamper the signed payload after mapping; the signature no longer
	// matches the content, so VerifySignature must reject it.
	tampered := []byte(`{"version":"v1","id":"msg-1","thread_id":"thread-1","intent":"assert","epistemic":"assumed","confidence":0.5,"payload":"tampered","signer":"` + msg.Signer + `","signature":"` + msg.Signature + `"}`)
	mapped.Part.Data = tampered

	tr := &stubTransport{
		taskID: "task-1",
		states: []State{StateCompleted},
		result: mapped,
	}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	h, err := c.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	_, err = c.Result(context.Background(), h)
	if err == nil {
		t.Fatal("Result accepted a tampered signature")
	}
	if !errors.Is(err, ErrSignatureCheckFailed) {
		t.Fatalf("Result error = %v, want errors.Is ErrSignatureCheckFailed", err)
	}
	// envelope.VerifySignature returns a plain, non-sentinel error, so
	// the second %w only proves the underlying detail text survives.
	const wantDetail = "signature does not match message content"
	if !strings.Contains(err.Error(), wantDetail) {
		t.Fatalf("Result error = %v, want it to contain %q", err, wantDetail)
	}
}

func TestResultPropagatesTransportFailure(t *testing.T) {
	tr := &stubTransport{
		taskID:    "task-1",
		states:    []State{StateCompleted},
		resultErr: errors.New("connection reset"),
	}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	h, err := c.Send(context.Background(), signedMessage(t))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := c.Result(context.Background(), h); err == nil {
		t.Fatal("Result accepted a transport failure")
	}
}

func TestSendRejectsEmptyTaskID(t *testing.T) {
	tr := &stubTransport{taskID: ""}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	h, err := c.Send(context.Background(), signedMessage(t))
	if err == nil {
		t.Fatal("Send accepted an empty task id from the transport")
	}
	if !errors.Is(err, ErrNoTaskID) {
		t.Fatalf("Send error = %v, want errors.Is ErrNoTaskID", err)
	}
	if h != (TaskHandle{}) {
		t.Fatal("Send returned a non-zero TaskHandle on error")
	}
}

func TestResultPropagatesStateFailure(t *testing.T) {
	tr := &stubTransport{taskID: "task-1", stateErr: errors.New("unavailable")}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	h, err := c.Send(context.Background(), signedMessage(t))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := c.Result(context.Background(), h); err == nil {
		t.Fatal("Result accepted a State failure")
	}
}

func TestResultRejectsUnmappableData(t *testing.T) {
	tr := &stubTransport{
		taskID: "task-1",
		states: []State{StateCompleted},
		result: a2a.Mapped{Part: a2a.Part{Data: []byte("not json")}},
	}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	h, err := c.Send(context.Background(), signedMessage(t))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := c.Result(context.Background(), h); err == nil {
		t.Fatal("Result accepted data a2a.FromPart cannot map")
	}
}

func TestStatusPropagatesTransportFailure(t *testing.T) {
	tr := &stubTransport{taskID: "task-1", stateErr: errors.New("unavailable")}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	h, err := c.Send(context.Background(), signedMessage(t))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := c.Status(context.Background(), h); err == nil {
		t.Fatal("Status accepted a transport failure")
	}
}

// TestSendEnforcesCanceledContextItself uses a transport that ignores
// ctx entirely, so this test can only pass if Client.Send performs its
// own ctx.Err() check before calling the transport.
func TestSendEnforcesCanceledContextItself(t *testing.T) {
	tr := &stubTransport{taskID: "task-1", ignoreCtx: true}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Send(ctx, signedMessage(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Send error = %v, want context.Canceled", err)
	}
}

// TestStatusEnforcesDeadlineBetweenPolls polls Status twice against a
// transport that ignores ctx entirely: the first call happens before
// the deadline expires and succeeds, and the second happens after it
// expires. This can only pass if Client.Status checks ctx itself on
// every call, not only relying on the transport.
func TestStatusEnforcesDeadlineBetweenPolls(t *testing.T) {
	tr := &stubTransport{taskID: "task-1", states: []State{StateWorking, StateWorking}, ignoreCtx: true}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	h, err := c.Send(context.Background(), signedMessage(t))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	ctx := &expiresAfterCtx{Context: context.Background(), callsBeforeExpiry: 1}
	state, err := c.Status(ctx, h)
	if err != nil {
		t.Fatalf("first Status call: %v", err)
	}
	if state != StateWorking {
		t.Fatalf("first Status call = %s, want %s", state, StateWorking)
	}
	if _, err := c.Status(ctx, h); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Status call error = %v, want context.DeadlineExceeded", err)
	}
}

// TestResultEnforcesExpiredDeadlineItself uses a transport that
// ignores ctx entirely, so this test can only pass if Client.Result
// performs its own ctx.Err() check before calling the transport.
func TestResultEnforcesExpiredDeadlineItself(t *testing.T) {
	tr := &stubTransport{taskID: "task-1", states: []State{StateCompleted}, ignoreCtx: true}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	h, err := c.Send(context.Background(), signedMessage(t))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Minute))
	defer cancel()
	if _, err := c.Result(ctx, h); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Result error = %v, want context.DeadlineExceeded", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	tr := &stubTransport{taskID: "task-1"}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := tr.closeCalls.Load(); got != 1 {
		t.Fatalf("transport Close called %d times, want 1", got)
	}
}
