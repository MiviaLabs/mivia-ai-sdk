package a2aclient

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"sync"

	a2acore "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2agrpc"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"google.golang.org/grpc"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// loopbackBind is the only address the loopback fixture binds. The
// kernel picks the port, so no external network dependence exists.
const loopbackBind = "127.0.0.1:0"

// loopbackExecutor completes each task with a result message carrying a
// freshly signed envelope that restates the request's payload. The
// response envelope binds its ID and ThreadID to the A2A ids the server
// mints, exactly as a real responding agent must, so the post-hop
// signature check passes. See grpc_loopback_integration_test.go for the
// same executor used in-package.
type loopbackExecutor struct {
	key ed25519.PrivateKey
}

var _ a2asrv.AgentExecutor = (*loopbackExecutor)(nil)

// Execute writes one completed status event whose message carries a
// signed envelope restating the request's payload.
func (e *loopbackExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	payload, err := loopbackPayload(reqCtx)
	if err != nil {
		return err
	}
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

// Cancel writes a canceled status event. The fixture never calls it;
// the interface requires it.
func (e *loopbackExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	event := a2acore.NewStatusUpdateEvent(reqCtx, a2acore.TaskStateCanceled, nil)
	event.Final = true
	return queue.Write(ctx, event)
}

// loopbackPayload finds the payload string of the request's first data
// part.
func loopbackPayload(reqCtx *a2asrv.RequestContext) (string, error) {
	if reqCtx.Message == nil {
		return "", errors.New("loopback: request carries no message")
	}
	for _, p := range reqCtx.Message.Parts {
		if dp, ok := p.(a2acore.DataPart); ok {
			if s, ok := dp.Data["payload"].(string); ok {
				return s, nil
			}
		}
	}
	return "", errors.New("loopback: request carries no payload")
}

// Loopback starts a gRPC A2A server on a 127.0.0.1 loopback port. It
// returns the address and a stop function. Each received task completes
// promptly. It ships for cross-package tests; the a2a-go exception
// covers a2aclient/*.go only, so the server handler must live in
// a2aclient's non-test source.
func Loopback() (addr string, stop func() error, err error) {
	lis, err := net.Listen("tcp", loopbackBind)
	if err != nil {
		return "", nil, err
	}
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		_ = lis.Close()
		return "", nil, err
	}
	server := grpc.NewServer()
	a2agrpc.NewHandler(a2asrv.NewHandler(&loopbackExecutor{key: key})).RegisterWith(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(lis)
	}()
	var once sync.Once
	stop = func() error {
		once.Do(func() {
			server.Stop()
			<-done
		})
		return nil
	}
	return lis.Addr().String(), stop, nil
}
