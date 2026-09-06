package a2aloopback

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
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
// freshly signed envelope that restates the request's payload and
// MaxHops. The response envelope binds its ID and ThreadID to the A2A
// ids the server mints, exactly as a real responding agent must, so
// the post-hop signature check passes.
type loopbackExecutor struct {
	key ed25519.PrivateKey
}

var _ a2asrv.AgentExecutor = (*loopbackExecutor)(nil)

// Execute writes one completed status event whose message carries a
// signed envelope restating the request's payload and MaxHops.
func (e *loopbackExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	req, err := loopbackRequest(reqCtx)
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
		Payload:    req.Payload,
		MaxHops:    req.MaxHops,
	})
	if err != nil {
		return err
	}
	mapped, err := a2a.ToPart(signed)
	if err != nil {
		return err
	}
	msg := &a2acore.Message{
		ID:        msgID,
		Role:      a2acore.MessageRoleAgent,
		TaskID:    info.TaskID,
		ContextID: info.ContextID,
		Parts:     a2acore.ContentParts{a2acore.TextPart{Text: mapped.Part.Text}},
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

// loopbackRequest decodes the request's first a2acore.TextPart into
// an envelope.Message. The executor restates the decoded Payload and
// MaxHops in its response envelope.
func loopbackRequest(reqCtx *a2asrv.RequestContext) (envelope.Message, error) {
	if reqCtx.Message == nil {
		return envelope.Message{}, errors.New("loopback: request carries no message")
	}
	for _, p := range reqCtx.Message.Parts {
		if tp, ok := p.(a2acore.TextPart); ok {
			var m envelope.Message
			if err := json.Unmarshal([]byte(tp.Text), &m); err != nil {
				return envelope.Message{}, fmt.Errorf("loopback: decode request: %w", err)
			}
			return m, nil
		}
	}
	return envelope.Message{}, errors.New("loopback: request carries no payload")
}

// Loopback starts a gRPC A2A server on a 127.0.0.1 loopback port. It
// returns the address and a stop function. Each received task completes
// promptly. No production package may import this package; see the
// package doc comment.
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
