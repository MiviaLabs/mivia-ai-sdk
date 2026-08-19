package dispatch

import (
	"context"
	"errors"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
)

// replayKey builds the ledger.IdempotencyKey for m. The len(ThreadID)
// prefix, terminated by the first ':', disambiguates the ThreadID/ID
// boundary: a reader parses the leading digits up to ':', takes
// exactly that many bytes as ThreadID, and the remainder as ID. ID is
// unique only within ThreadID (envelope/message.go), so the key must
// carry both.
func replayKey(m envelope.Message) ledger.IdempotencyKey {
	return ledger.IdempotencyKey(fmt.Sprintf("%d:%s%s", len(m.ThreadID), m.ThreadID, m.ID))
}

// replaySentinels lists every taskrun.Run outcome that means "this
// key already has, or is already getting, an admitted outcome": a
// terminal record, or a live claim held by an in-flight duplicate.
// ledger.ErrNotClaimed covers the race window between taskrun.Run's
// own State check and its Claim call: a concurrent duplicate can pass
// State while the record still reads Pending, then find it already
// Completed by the time its own Claim runs, which Claim reports
// through its default terminal-status branch as ErrNotClaimed rather
// than one of the three taskrun sentinels. isReplay walks this list
// instead of a chained boolean expression, so a mutation to one
// comparison cannot regroup neighboring terms through operator
// precedence and stay undetected.
var replaySentinels = []error{
	taskrun.ErrTaskDone,
	taskrun.ErrTaskFailed,
	taskrun.ErrTaskBlocked,
	ledger.ErrLeaseActive,
	ledger.ErrNotClaimed,
}

// isReplay reports whether err matches one of replaySentinels.
func isReplay(err error) bool {
	for _, sentinel := range replaySentinels {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

// processLine runs the receive ladder over one NDJSON line and
// returns the reply line: a confirmed ack on success, or a JSON
// error object naming the failing stage. Each stage is fail-fast;
// EmitMessageDelivered and EmitMessageAcked are best-effort
// diagnostics, called after their point in the ladder with their
// error return ignored. Resolve, handle, and ack construction run
// once per replay key, guarded by taskrun.Run; a duplicate key
// answers a "replay:" error line instead of running them again.
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

	var ack envelope.Ack
	work := func(ctx context.Context) error {
		h, err := e.resolve(ctx, m)
		if err != nil {
			return fmt.Errorf("resolve: %w", err)
		}
		restatement, err := h.Handle(ctx, m)
		if err != nil {
			return fmt.Errorf("handle: %w", err)
		}
		ack, err = envelope.NewAck(m, e.id, restatement)
		if err != nil {
			return fmt.Errorf("handle: %w", err)
		}
		return nil
	}
	task := taskrun.Task{Key: replayKey(m), Seq: 1, Description: string(m.Intent)}
	if err := taskrun.Run(ctx, e.taskOpts, task, work); err != nil {
		if isReplay(err) {
			return encodeErrorLine(fmt.Errorf("replay: %w", ErrReplay))
		}
		return encodeErrorLine(err)
	}
	ack = ack.Confirm()
	_ = agent.EmitMessageAcked(ctx, e.bus, ack)
	// out cannot fail: ack comes from a validated NewAck plus Confirm,
	// which always yields a Validate-clean Ack for Encode to marshal.
	out, _ := ack.Encode()
	return out
}
