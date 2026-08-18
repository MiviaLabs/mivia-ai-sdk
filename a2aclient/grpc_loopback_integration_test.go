package a2aclient

import (
	"context"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// loopbackClient boots the exported Loopback fixture and returns a
// Client pointed at it. It registers cleanup that stops the server and
// closes the client. Using Loopback keeps the fixture covered and
// removes the duplicated in-package server this file once carried.
func loopbackClient(t *testing.T) *Client {
	t.Helper()
	addr, stop, err := Loopback()
	if err != nil {
		t.Fatalf("Loopback: %v", err)
	}
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("Loopback stop: %v", err)
		}
	})
	c, err := New(addr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return c
}

// mustBeCompleted reads the task's state once and requires it to be
// StateCompleted already.
//
// No poll loop is needed and none is used. a2a-go's non-streaming
// SendMessage returns only after the executor writes its final event,
// so the task is terminal the moment Send returns. Asserting that on
// the first read keeps the test deterministic, with no sleep and no
// timing assumption.
func mustBeCompleted(t *testing.T, c *Client, h TaskHandle) State {
	t.Helper()
	state, err := c.Status(context.Background(), h)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state != StateCompleted {
		t.Fatalf("Status = %s immediately after Send, want %s", state, StateCompleted)
	}
	return state
}

// TestGRPCLoopbackRoundTrip runs the full Send, Status, Result
// sequence over the exported Loopback fixture. It proves the dial, the
// wire round trip, and the post-hop signature re-verification all work
// against a real gRPC server, which the stub transport in
// stub_transport_test.go cannot prove.
func TestGRPCLoopbackRoundTrip(t *testing.T) {
	c := loopbackClient(t)

	sent := signedMessage(t)
	h, err := c.Send(context.Background(), sent)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	mustBeCompleted(t, c, h)

	got, err := c.Result(context.Background(), h)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	// Result already ran VerifySignature; running it again here states
	// the invariant this test exists to prove.
	if err := got.VerifySignature(); err != nil {
		t.Fatalf("VerifySignature after the real gRPC hop: %v", err)
	}
	if got.Payload != sent.Payload {
		t.Fatalf("Result payload = %q, want %q", got.Payload, sent.Payload)
	}
	// The result is the remote agent's own signed envelope, not a copy
	// of the request. Its ID and ThreadID are the A2A message and
	// context ids the server minted, which is what FromPart stamps in.
	if got.ID == "" {
		t.Fatal("Result message id is empty, want the server's A2A message id")
	}
	if got.ID == sent.ID {
		t.Fatalf("Result message id = %q, want the server's own id, not the request's", got.ID)
	}
	if got.ThreadID == "" {
		t.Fatal("Result thread id is empty, want the server's A2A context id")
	}
	if got.Signer == sent.Signer {
		t.Fatal("Result signer equals the caller's signer, want the remote agent's own key")
	}
}

// TestGRPCLoopbackConcurrentClients runs eight goroutines through the
// full Send, Status, Result sequence on one shared Client over the
// real transport. Run under go test -race.
func TestGRPCLoopbackConcurrentClients(t *testing.T) {
	c := loopbackClient(t)

	const goroutines = 8
	sent := signedMessage(t)
	var wg sync.WaitGroup
	results := make([]envelope.Message, goroutines)
	errs := make([]error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h, err := c.Send(context.Background(), sent)
			if err != nil {
				errs[i] = err
				return
			}
			mustBeCompleted(t, c, h)
			results[i], errs[i] = c.Result(context.Background(), h)
		}(i)
	}
	wg.Wait()

	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if err := results[i].VerifySignature(); err != nil {
			t.Fatalf("goroutine %d: VerifySignature after the real gRPC hop: %v", i, err)
		}
		if results[i].Payload != sent.Payload {
			t.Fatalf("goroutine %d payload = %q, want %q", i, results[i].Payload, sent.Payload)
		}
	}
}
