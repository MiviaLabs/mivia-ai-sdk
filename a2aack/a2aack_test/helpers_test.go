// Package a2aack_test holds the external tests for the a2aack package.
// A fake Remote drives every loop, timing, and error outcome; the live
// a2aclient.Loopback fixture appears only in the happy-path and
// integration tests. No test file imports a2a-go, so the Semgrep
// stdlib-only rule holds outside a2aclient.
package a2aack_test

import (
	"context"
	"crypto/ed25519"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/a2aclient"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// signedMessage returns a valid, signed envelope.Message for tests
// that need one to send through a Remote.
func signedMessage(t testing.TB) envelope.Message {
	t.Helper()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signed, err := envelope.Sign(key, envelope.Message{
		Version:    envelope.Version,
		ID:         "msg-1",
		ThreadID:   "thread-1",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicAssumed,
		Confidence: 0.5,
		Payload:    "hello remote agent",
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

// signedResult returns a freshly signed result message the fake Remote
// can hand back on a completed task.
func signedResult(t testing.TB) envelope.Message {
	t.Helper()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate result key: %v", err)
	}
	signed, err := envelope.Sign(key, envelope.Message{
		Version:    envelope.Version,
		ID:         "res-1",
		ThreadID:   "thread-1",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicAssumed,
		Confidence: 0.5,
		Payload:    "remote finished",
	})
	if err != nil {
		t.Fatalf("sign result: %v", err)
	}
	return signed
}

// fakeRemote scripts a Remote round trip from exported fields. Its
// sendErr, statusStates, statusErr, result, and resultErr fields set
// the outcome of each Send, Status, and Result call. Status consumes
// statusStates one entry per call and repeats the last entry once the
// script is exhausted. sendCalls and statusCalls count their calls.
type fakeRemote struct {
	sendErr      error
	sendCalls    atomic.Int64
	statusErr    error
	statusStates []a2aclient.State
	statusCalls  atomic.Int64
	result       envelope.Message
	resultErr    error
	// cancelOnStatus, when set, runs on every Status call before the
	// scripted state returns. Tests use it to cancel ctx mid-loop
	// without a sleep.
	cancelOnStatus func()
}

// Send records the call and returns sendErr or a zero task handle.
func (f *fakeRemote) Send(context.Context, envelope.Message) (a2aclient.TaskHandle, error) {
	f.sendCalls.Add(1)
	if f.sendErr != nil {
		return a2aclient.TaskHandle{}, f.sendErr
	}
	return a2aclient.TaskHandle{}, nil
}

// Status records the call, runs cancelOnStatus when set, and steps
// through statusStates.
func (f *fakeRemote) Status(context.Context, a2aclient.TaskHandle) (a2aclient.State, error) {
	f.statusCalls.Add(1)
	if f.cancelOnStatus != nil {
		f.cancelOnStatus()
	}
	if f.statusErr != nil {
		return a2aclient.StateUnspecified, f.statusErr
	}
	if len(f.statusStates) == 0 {
		return a2aclient.StateUnspecified, nil
	}
	idx := int(f.statusCalls.Load()) - 1
	if idx >= len(f.statusStates) {
		idx = len(f.statusStates) - 1
	}
	return f.statusStates[idx], nil
}

// Result returns the scripted result or resultErr.
func (f *fakeRemote) Result(context.Context, a2aclient.TaskHandle) (envelope.Message, error) {
	if f.resultErr != nil {
		return envelope.Message{}, f.resultErr
	}
	return f.result, nil
}
