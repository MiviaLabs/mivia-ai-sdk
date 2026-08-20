// Mailbox is the in-process message plane between an orchestrator
// and its subagents, and between a human and a subagent.

package subagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// ErrMailboxFull reports a Deliver against a full mailbox.
var ErrMailboxFull = errors.New("subagent: mailbox is full")

// ErrInvalidCapacity reports a NewMailbox call whose capacity is not
// positive. Test with errors.Is.
var ErrInvalidCapacity = errors.New("subagent: mailbox capacity must be positive")

// ErrUnverified reports a Deliver whose message fails envelope
// signature verification. Test with errors.Is.
var ErrUnverified = errors.New("subagent: mailbox rejects an unverified message")

// Mailbox holds signed messages for one recipient. It is safe for
// concurrent use. Deliver validates, verifies the signature, and
// appends; Take drains.
type Mailbox struct {
	mu   sync.Mutex
	msgs []envelope.Message
	cap  int
}

// NewMailbox builds a mailbox holding at most capacity messages.
func NewMailbox(capacity int) (*Mailbox, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("subagent: mailbox capacity %d must be positive: %w", capacity, ErrInvalidCapacity)
	}
	return &Mailbox{cap: capacity}, nil
}

// Deliver validates msg, verifies its signature, and appends it. An
// unsigned or tampered message fails with ErrUnverified. A full
// mailbox fails with ErrMailboxFull.
func (m *Mailbox) Deliver(msg envelope.Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	if err := msg.VerifySignature(); err != nil {
		return fmt.Errorf("%w: %v", ErrUnverified, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.msgs) >= m.cap {
		return ErrMailboxFull
	}
	m.msgs = append(m.msgs, msg)
	return nil
}

// Take drains every held message in delivery order.
func (m *Mailbox) Take() []envelope.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.msgs
	m.msgs = nil
	return out
}

// sendSeq numbers sent messages uniquely per process.
var sendSeq uint64

// SendTool returns a tool that signs one message per call with id and
// delivers it to the bound mailbox: any caller - agent or human
// wiring - sends to the recipient through the same surface. The
// input string is the payload; the result is the message id.
func SendTool(name string, box *Mailbox, id *identity.Identity) tools.Tool {
	return &sendTool{name: name, box: box, id: id}
}

// sendTool adapts one identity and mailbox to the tools.Tool
// interface.
type sendTool struct {
	name string
	box  *Mailbox
	id   *identity.Identity
}

// Name returns the registry name.
func (t *sendTool) Name() string { return t.name }

// Run builds, signs, and delivers one message.
func (t *sendTool) Run(_ context.Context, in tools.InOut) (tools.Out, error) {
	msg := envelope.Message{
		Version:   envelope.Version,
		ID:        fmt.Sprintf("%s-%d", t.name, atomic.AddUint64(&sendSeq, 1)),
		ThreadID:  t.name,
		Intent:    envelope.IntentRequest,
		Epistemic: envelope.EpistemicAssumed,
		Payload:   stringValue(in),
	}
	signed, err := t.id.Sign(msg)
	if err != nil {
		return tools.Out{}, err
	}
	if err := t.box.Deliver(signed); err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: signed.ID}, nil
}

// inboxEmpty reports an inbox with nothing pending.
const inboxEmpty = "empty"

// InboxTool returns a tool bound to one mailbox. Each Run drains the
// mailbox and returns its payloads comma-joined; an empty mailbox
// returns the literal empty marker.
func InboxTool(name string, box *Mailbox) tools.Tool {
	return &inboxTool{name: name, box: box}
}

// inboxTool adapts one mailbox to the tools.Tool interface.
type inboxTool struct {
	name string
	box  *Mailbox
}

// Name returns the registry name.
func (t *inboxTool) Name() string { return t.name }

// Run drains the mailbox and reports its payloads.
func (t *inboxTool) Run(_ context.Context, in tools.InOut) (tools.Out, error) {
	msgs := t.box.Take()
	if len(msgs) == 0 {
		return tools.Out{Value: inboxEmpty}, nil
	}
	payloads := make([]string, 0, len(msgs))
	for _, m := range msgs {
		payloads = append(payloads, m.Payload)
	}
	return tools.Out{Value: strings.Join(payloads, ",")}, nil
}
