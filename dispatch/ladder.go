package dispatch

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// processLine runs the receive ladder over one NDJSON line and
// returns the reply line: a confirmed ack on success, or a JSON
// error object naming the failing stage. Each stage is fail-fast;
// EmitMessageDelivered and EmitMessageAcked are best-effort
// diagnostics, called after their point in the ladder with their
// error return ignored.
func (e *Endpoint) processLine(ctx context.Context, line []byte) []byte {
	m, err := envelope.Decode(line)
	if err != nil {
		return encodeErrorLine(fmt.Errorf("decode: %w", err))
	}
	if err := m.VerifySignature(); err != nil {
		return encodeErrorLine(fmt.Errorf("verify: %w", err))
	}
	_ = agent.EmitMessageDelivered(ctx, e.bus, m)
	if err := e.room.Accepts(m); err != nil {
		return encodeErrorLine(fmt.Errorf("admit: %w", err))
	}
	h, err := e.resolve(ctx, m)
	if err != nil {
		return encodeErrorLine(fmt.Errorf("resolve: %w", err))
	}
	restatement, err := h.Handle(ctx, m)
	if err != nil {
		return encodeErrorLine(fmt.Errorf("handle: %w", err))
	}
	ack, err := envelope.NewAck(m, e.id, restatement)
	if err != nil {
		return encodeErrorLine(fmt.Errorf("handle: %w", err))
	}
	ack = ack.Confirm()
	_ = agent.EmitMessageAcked(ctx, e.bus, ack)
	// out cannot fail: ack comes from a validated NewAck plus Confirm,
	// which always yields a Validate-clean Ack for Encode to marshal.
	out, _ := ack.Encode()
	return out
}
