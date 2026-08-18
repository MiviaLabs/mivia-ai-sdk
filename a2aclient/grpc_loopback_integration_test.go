package a2aclient

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"sync"
	"testing"

	a2acore "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2agrpc"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"google.golang.org/grpc"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// loopbackHost is the only address this file ever binds. The suite
// takes no external network dependency; the kernel picks the port.
const loopbackHost = "127.0.0.1:0"

// echoExecutor is the minimal a2asrv.AgentExecutor this test needs. It
// completes each task with a result message carrying a freshly signed
// envelope that restates the request's payload.
//
// The executor signs a response bound to the A2A ids, not a copy of
// the request envelope. a2a.FromPart overwrites the decoded message's
// ThreadID from Mapped.ContextID and its ID from Mapped.MessageID, and
// an A2A server mints both of those itself. A remote agent that echoed
// the caller's envelope verbatim would therefore fail the client's
// post-hop signature check, because FromPart would replace two signed
// fields with the server's own ids. Binding the response envelope's ID
// and ThreadID to the A2A message and context ids is what a real
// responding agent must do. See a2a/part.go's FromPart.
type echoExecutor struct {
	key ed25519.PrivateKey
}

var _ a2asrv.AgentExecutor = (*echoExecutor)(nil)

// Execute writes one final completed status event whose message
// carries a signed envelope restating the request's payload.
func (e *echoExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	data, err := requestData(reqCtx)
	if err != nil {
		return err
	}
	payload, _ := data["payload"].(string)
	info := reqCtx.TaskInfo()
	msgID := a2acore.NewMessageID()
	signed, err := envelope.Sign(e.key, envelope.Message{
		Version:    envelope.Version,
		ID:         msgID,
		ThreadID:   info.ContextID,
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicAssumed,
		Confidence: 0.5,
		Payload:    payload,
	})
	if err != nil {
		return err
	}
	mapped, err := a2a.ToPart(signed)
	if err != nil {
		return err
	}
	body, err := dataFromRaw(mapped.Part.Data)
	if err != nil {
		return err
	}
	msg := &a2acore.Message{
		ID:        msgID,
		Role:      a2acore.MessageRoleAgent,
		TaskID:    info.TaskID,
		ContextID: info.ContextID,
		Parts:     a2acore.ContentParts{a2acore.DataPart{Data: body}},
	}
	event := a2acore.NewStatusUpdateEvent(reqCtx, a2acore.TaskStateCompleted, msg)
	event.Final = true
	return queue.Write(ctx, event)
}

// Cancel writes a final canceled status event. The suite never calls
// it; the interface requires it.
func (e *echoExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	event := a2acore.NewStatusUpdateEvent(reqCtx, a2acore.TaskStateCanceled, nil)
	event.Final = true
	return queue.Write(ctx, event)
}

// requestData returns the first DataPart payload of the message that
// triggered this execution.
func requestData(reqCtx *a2asrv.RequestContext) (map[string]any, error) {
	if reqCtx.Message == nil {
		return nil, errors.New("echoExecutor: request carries no message")
	}
	for _, p := range reqCtx.Message.Parts {
		if dp, ok := p.(a2acore.DataPart); ok {
			return dp.Data, nil
		}
	}
	return nil, errors.New("echoExecutor: request message carries no data part")
}

// startLoopbackServer starts an a2a-go gRPC server on a loopback port
// and returns its address. It registers cleanup that stops the server
// before the test ends.
func startLoopbackServer(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", loopbackHost)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	server := grpc.NewServer()
	a2agrpc.NewHandler(a2asrv.NewHandler(&echoExecutor{key: key})).RegisterWith(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(lis)
	}()
	t.Cleanup(func() {
		server.Stop()
		<-done
	})
	return lis.Addr().String()
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
// sequence over a real a2a-go gRPC transport on a loopback listener.
// It proves the dial, the wire round trip, and the post-hop signature
// re-verification all work against a real server, which the stub
// transport in stub_transport_test.go cannot prove.
func TestGRPCLoopbackRoundTrip(t *testing.T) {
	addr := startLoopbackServer(t)
	c, err := New(addr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

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
	addr := startLoopbackServer(t)
	c, err := New(addr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

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
