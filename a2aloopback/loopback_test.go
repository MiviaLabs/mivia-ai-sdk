package a2aloopback

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	a2acore "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"

	"github.com/MiviaLabs/mivia-ai-sdk/a2aclient"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// failingReader always fails Read, for exercising the ed25519 key
// generation error path.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("failingReader: read failed")
}

// signedMessage returns a valid, signed envelope.Message for tests
// that need one to send through a Client.
func signedMessage(t *testing.T) envelope.Message {
	t.Helper()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	msg, err := envelope.Sign(key, envelope.Message{
		Version:    envelope.Version,
		ID:         "msg-1",
		ThreadID:   "thread-1",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicAssumed,
		Confidence: 0.5,
		Payload:    "hello",
	})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return msg
}

// TestLoopbackRoundTrip starts Loopback, sends one signed message
// through a real a2aclient.Client, and asserts the reply verifies and
// carries the same payload. This exercises Loopback,
// loopbackExecutor.Execute, and loopbackPayload's success path.
func TestLoopbackRoundTrip(t *testing.T) {
	addr, stop, err := Loopback()
	if err != nil {
		t.Fatalf("Loopback: %v", err)
	}
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("stop: %v", err)
		}
	})

	c, err := a2aclient.New(addr)
	if err != nil {
		t.Fatalf("a2aclient.New: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	sent := signedMessage(t)
	h, err := c.Send(context.Background(), sent)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	state, err := c.Status(context.Background(), h)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state != a2aclient.StateCompleted {
		t.Fatalf("Status = %s immediately after Send, want %s", state, a2aclient.StateCompleted)
	}

	got, err := c.Result(context.Background(), h)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if err := got.VerifySignature(); err != nil {
		t.Fatalf("VerifySignature after the real gRPC hop: %v", err)
	}
	if got.Payload != sent.Payload {
		t.Fatalf("Result payload = %q, want %q", got.Payload, sent.Payload)
	}
}

// TestLoopbackExecutorCancel builds a loopbackExecutor directly and
// calls Cancel with a real in-memory eventqueue.Queue, asserting a
// canceled, final status event is written. Loopback's own server flow
// never reaches Cancel; this is the only way to cover it.
func TestLoopbackExecutorCancel(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	e := &loopbackExecutor{key: key}

	taskID := a2acore.NewTaskID()
	mgr := eventqueue.NewInMemoryManager()
	ctx := context.Background()
	writer, err := mgr.GetOrCreate(ctx, taskID)
	if err != nil {
		t.Fatalf("GetOrCreate writer: %v", err)
	}
	reader, err := mgr.GetOrCreate(ctx, taskID)
	if err != nil {
		t.Fatalf("GetOrCreate reader: %v", err)
	}

	reqCtx := &a2asrv.RequestContext{TaskID: taskID, ContextID: string(a2acore.NewContextID())}

	errCh := make(chan error, 1)
	go func() {
		errCh <- e.Cancel(ctx, reqCtx, writer)
	}()

	event, _, err := reader.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	update, ok := event.(*a2acore.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("event = %T, want *a2acore.TaskStatusUpdateEvent", event)
	}
	if !update.Final {
		t.Fatal("update.Final = false, want true")
	}
	if update.Status.State != a2acore.TaskStateCanceled {
		t.Fatalf("update.Status.State = %s, want %s", update.Status.State, a2acore.TaskStateCanceled)
	}
}

// TestLoopbackExecutorExecuteRejectsMissingContextID exercises
// Execute's a2a.ToPart error branch: an empty ContextID maps to an
// empty envelope.Message.ThreadID, which Validate rejects. Unlike
// loopbackPayload's two branches, this path is reachable through the
// real a2a-go server: a malformed RequestContext with no ContextID.
func TestLoopbackExecutorExecuteRejectsMissingContextID(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	e := &loopbackExecutor{key: key}
	reqCtx := &a2asrv.RequestContext{
		TaskID: a2acore.NewTaskID(),
		Message: &a2acore.Message{
			Parts: a2acore.ContentParts{a2acore.DataPart{Data: map[string]any{"payload": "hello"}}},
		},
	}
	if err := e.Execute(context.Background(), reqCtx, nil); err == nil {
		t.Fatal("Execute with an empty ContextID succeeded, want an error")
	}
}

// TestDataFromRawRejectsMalformedJSON exercises dataFromRaw's own
// json.Unmarshal error branch directly; no real caller produces this
// input, since a2a.ToPart's own Encode always emits valid JSON.
func TestDataFromRawRejectsMalformedJSON(t *testing.T) {
	if _, err := dataFromRaw([]byte("{not json")); err == nil {
		t.Fatal("dataFromRaw with malformed JSON succeeded, want an error")
	}
}

// TestLoopbackRejectsKeyGenerationFailure exercises Loopback's
// ed25519.GenerateKey error branch, and that it closes the listener it
// already opened before returning the error.
func TestLoopbackRejectsKeyGenerationFailure(t *testing.T) {
	orig := rand.Reader
	rand.Reader = failingReader{}
	defer func() { rand.Reader = orig }()

	addr, stop, err := Loopback()
	if err == nil {
		t.Fatal("Loopback with a failing rand.Reader succeeded, want an error")
	}
	if addr != "" {
		t.Fatalf("addr = %q, want empty on failure", addr)
	}
	if stop != nil {
		t.Fatal("stop is non-nil, want nil on failure")
	}
}

// TestLoopbackStopIsIdempotent calls stop twice and asserts both calls
// return nil, mirroring a2aclient.Client.Close's idempotency contract
// this fixture's caller relies on.
func TestLoopbackStopIsIdempotent(t *testing.T) {
	_, stop, err := Loopback()
	if err != nil {
		t.Fatalf("Loopback: %v", err)
	}
	if err := stop(); err != nil {
		t.Fatalf("first stop: %v", err)
	}
	if err := stop(); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}
