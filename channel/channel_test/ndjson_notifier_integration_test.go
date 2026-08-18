package channel_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// ndjsonAckWaitFrom is the fixed identity string the wrapper closure
// supplies for envelope.Ack.From, a field neither channel.Question
// nor channel.Answer carries.
const ndjsonAckWaitFrom = "desktop-app"

// TestNDJSONNotifierBacksAckWait proves an NDJSON transport built by
// NewNDJSONNotifier composes with the same real agent.AckWait call
// site notifier_integration_test.go already proves for a plain
// closure, over a real io.Pipe pair standing in for the desktop app's
// stdio pipe.
func TestNDJSONNotifierBacksAckWait(t *testing.T) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	// The peer side, standing in for the desktop app: reads one
	// question line, then writes back a matching answer line.
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		if !sc.Scan() {
			return
		}
		var q ndjsonLine
		if err := json.Unmarshal(sc.Bytes(), &q); err != nil {
			return
		}
		reply := ndjsonLine{Type: "answer", QuestionID: q.ID, Approved: true, Payload: "looks good"}
		_ = json.NewEncoder(aw).Encode(reply)
	}()

	var wait agent.AckWait = func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		q := channel.Question{
			ID:        msg.ID,
			Recipient: ndjsonAckWaitFrom,
			Payload:   msg.Payload,
		}
		if err := q.Validate(); err != nil {
			return envelope.Ack{}, err
		}
		ans, err := notify(ctx, q)
		if err != nil {
			return envelope.Ack{}, err
		}
		ack, err := envelope.NewAck(msg, ndjsonAckWaitFrom, ans.Payload)
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
		ID:         "msg-ndjson-1",
		ThreadID:   "thread-ndjson-1",
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
	if ack.From != ndjsonAckWaitFrom {
		t.Fatalf("From = %q, want %q", ack.From, ndjsonAckWaitFrom)
	}
	if ack.Status != envelope.AckConfirmed {
		t.Fatalf("Status = %q, want %q", ack.Status, envelope.AckConfirmed)
	}
	if ack.Restatement != "looks good" {
		t.Fatalf("Restatement = %q, want %q", ack.Restatement, "looks good")
	}
}
