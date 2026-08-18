// ChannelTool asks a human from a tool call.

package subagent

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// askSeq numbers questions uniquely per process.
var askSeq uint64

// ChannelTool returns a tool that routes its input string to one
// human as a question. An approved answer returns its payload, or
// approved when the answer carries none. A declined answer fails
// the call naming the recipient.
func ChannelTool(name string, ask channel.Notifier, recipient string) tools.Tool {
	return &channelTool{name: name, ask: ask, recipient: recipient}
}

// channelTool adapts one Notifier to the tools.Tool interface.
type channelTool struct {
	name      string
	ask       channel.Notifier
	recipient string
}

// Name returns the registry name.
func (t *channelTool) Name() string { return t.name }

// Run asks once and returns the answer's payload.
func (t *channelTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	q := channel.Question{
		ID:        fmt.Sprintf("%s-%d", t.name, atomic.AddUint64(&askSeq, 1)),
		Recipient: t.recipient,
		Payload:   stringValue(in),
	}
	answer, err := t.ask(ctx, q)
	if err != nil {
		return tools.Out{}, err
	}
	if !answer.Approved {
		return tools.Out{}, fmt.Errorf("subagent: %s: declined by %q", t.name, t.recipient)
	}
	if answer.Payload == "" {
		return tools.Out{Value: "approved"}, nil
	}
	return tools.Out{Value: answer.Payload}, nil
}
