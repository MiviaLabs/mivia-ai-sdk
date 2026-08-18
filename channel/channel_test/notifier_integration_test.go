package channel_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// ackWaitFromNotifier is the fixed identity string the wrapper
// closure supplies for envelope.Ack.From, a field neither
// channel.Question nor channel.Answer carries.
const ackWaitFromNotifier = "receiver-identity"

// TestNotifierBacksAckWait proves a channel.Notifier-backed closure
// satisfies agent.AckWait's exact signature. It wraps a
// channel.Notifier in a second closure that sources
// envelope.Ack.From from a fixed identity string, assigns the result
// to a variable of the real agent.AckWait type, and calls it with a
// real envelope.Message to check the resulting envelope.Ack.
func TestNotifierBacksAckWait(t *testing.T) {
	var notify channel.Notifier = func(ctx context.Context, q channel.Question) (channel.Answer, error) {
		return channel.Answer{
			QuestionID: q.ID,
			Approved:   true,
			Payload:    "looks good",
		}, nil
	}

	var wait agent.AckWait = func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		q := channel.Question{
			ID:        msg.ID,
			Recipient: ackWaitFromNotifier,
			Payload:   msg.Payload,
		}
		if err := q.Validate(); err != nil {
			return envelope.Ack{}, err
		}
		ans, err := notify(ctx, q)
		if err != nil {
			return envelope.Ack{}, err
		}
		if err := ans.Validate(); err != nil {
			return envelope.Ack{}, err
		}
		ack, err := envelope.NewAck(msg, ackWaitFromNotifier, ans.Payload)
		if err != nil {
			return envelope.Ack{}, err
		}
		if ans.Approved {
			ack = ack.Confirm()
		} else {
			ack = ack.Correct(ans.Payload)
		}
		return ack, nil
	}

	msg := envelope.Message{
		Version:    envelope.Version,
		ID:         "msg-1",
		ThreadID:   "thread-1",
		Intent:     envelope.IntentQuery,
		Epistemic:  envelope.EpistemicVerified,
		Confidence: 1,
		Payload:    "please review",
	}

	ack, err := wait(context.Background(), msg)
	if err != nil {
		t.Fatalf("wait() error = %v, want nil", err)
	}
	if ack.MessageID != msg.ID {
		t.Fatalf("MessageID = %q, want %q", ack.MessageID, msg.ID)
	}
	if ack.From != ackWaitFromNotifier {
		t.Fatalf("From = %q, want %q", ack.From, ackWaitFromNotifier)
	}
	if ack.Status != envelope.AckConfirmed {
		t.Fatalf("Status = %q, want %q", ack.Status, envelope.AckConfirmed)
	}
	if ack.Restatement != "looks good" {
		t.Fatalf("Restatement = %q, want %q", ack.Restatement, "looks good")
	}
}
