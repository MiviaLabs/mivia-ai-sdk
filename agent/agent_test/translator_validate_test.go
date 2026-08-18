// Package agent_test also holds the exhaustive rejection-branch cases
// for the envelope-to-events translator. translator_test.go proves
// one representative failure mode per EmitX function; the cases here
// prove every remaining Ack.Validate and VerifyThread rejection
// branch surfaces through the translator, unwrapped, so a translator
// that hand-rolled a partial check instead of delegating fully to
// Validate or VerifyThread cannot pass silently.
package agent_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
)

// unsignedMessage returns a Message with no Signer and no Signature,
// so VerifySignature must reject it on the "message is unsigned"
// branch, distinct from badSignatureMessage's tampered-content
// branch.
func unsignedMessage(id string) envelope.Message {
	return baseMessage(id)
}

// blankFromAck returns an Ack whose From fails Validate.
func blankFromAck() envelope.Ack {
	return envelope.Ack{
		MessageID:   "msg-1",
		From:        "   ",
		Restatement: "I will do X",
		Status:      envelope.AckPending,
	}
}

// blankRestatementAck returns an Ack whose Restatement fails
// Validate.
func blankRestatementAck() envelope.Ack {
	return envelope.Ack{
		MessageID:   "msg-1",
		From:        "receiver-key",
		Restatement: " ",
		Status:      envelope.AckPending,
	}
}

// invalidStatusAck returns an Ack whose Status is outside the
// AckStatus enum, so Validate rejects it on the default branch.
func invalidStatusAck() envelope.Ack {
	return envelope.Ack{
		MessageID:   "msg-1",
		From:        "receiver-key",
		Restatement: "I will do X",
		Status:      "bogus-status",
	}
}

// correctionOnPendingAck returns a pending Ack that also carries a
// Correction, which Validate rejects: Correction requires status
// AckCorrected.
func correctionOnPendingAck() envelope.Ack {
	return envelope.Ack{
		MessageID:   "msg-1",
		From:        "receiver-key",
		Restatement: "I will do X",
		Status:      envelope.AckPending,
		Correction:  "unexpected correction",
	}
}

// correctedWithNoCorrectionAck returns a corrected Ack with a blank
// Correction, which Validate rejects: AckCorrected requires a
// Correction.
func correctedWithNoCorrectionAck() envelope.Ack {
	return envelope.Ack{
		MessageID:   "msg-1",
		From:        "receiver-key",
		Restatement: "I will do X",
		Status:      envelope.AckCorrected,
		Correction:  "  ",
	}
}

// emptyThread returns a thread with no messages, so VerifyThread must
// reject it on the "thread is empty" branch, before it looks at any
// message field.
func emptyThread() []envelope.Message {
	return []envelope.Message{}
}

// duplicateIDThread returns a two-message thread where both messages
// share one ID, so VerifyThread must reject it on the duplicate-ID
// branch, distinct from brokenThread's PrevHash mismatch.
func duplicateIDThread() []envelope.Message {
	m1 := baseMessage("dup")
	m2 := baseMessage("dup")
	m2.PrevHash = m1.Hash()
	return []envelope.Message{m1, m2}
}

// threadIDMismatchThread returns a two-message thread where the
// second message names a different ThreadID than the first, so
// VerifyThread must reject it on the thread-mismatch branch.
func threadIDMismatchThread() []envelope.Message {
	m1 := baseMessage("t1")
	m2 := baseMessage("t2")
	m2.ThreadID = "a-different-thread"
	m2.PrevHash = m1.Hash()
	return []envelope.Message{m1, m2}
}

// firstMessagePrevHashThread returns a one-message thread whose sole
// message carries a non-empty PrevHash, so VerifyThread must reject
// it on the first-message-must-not-have-prev-hash branch.
func firstMessagePrevHashThread() []envelope.Message {
	m1 := baseMessage("t1")
	m1.PrevHash = envelope.ContextRef("a parent that does not exist")
	return []envelope.Message{m1}
}

// TestEmitMessageDeliveredUnsigned proves an unsigned Message (no
// Signer, no Signature) returns the VerifySignature error and runs no
// handler. This exercises VerifySignature's "message is unsigned"
// branch, distinct from TestEmitMessageDeliveredBadSignature's
// tampered-content branch: a translator that only checked for a
// tampered payload, and skipped delegating to VerifySignature for the
// unsigned case, would pass that test but fail this one.
func TestEmitMessageDeliveredUnsigned(t *testing.T) {
	bus := events.New()
	r := &recorder{}
	if err := bus.Subscribe(agent.MessageDeliveredEvent, r.handler); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}
	m := unsignedMessage("msg-unsigned")
	wantErr := m.VerifySignature()
	if wantErr == nil {
		t.Fatal("unsignedMessage() verified cleanly, want a verification error")
	}

	err := agent.EmitMessageDelivered(context.Background(), bus, m)
	if err == nil {
		t.Fatal("EmitMessageDelivered() returned a nil error, want the verification error")
	}
	if err.Error() != wantErr.Error() {
		t.Fatalf("EmitMessageDelivered() error = %q, want %q", err.Error(), wantErr.Error())
	}
	if got := r.count(); got != 0 {
		t.Fatalf("handler ran %d times, want 0", got)
	}
}

// TestEmitMessageAckedValidateFailureModes proves EmitMessageAcked
// surfaces every Ack.Validate rejection branch, unwrapped, and runs
// no handler for any of them. A translator that hand-checked only
// MessageID, instead of delegating fully to Validate, would pass
// TestEmitMessageAckedBlankMessageID but fail the cases here.
func TestEmitMessageAckedValidateFailureModes(t *testing.T) {
	cases := []struct {
		name string
		ack  envelope.Ack
	}{
		{"blank message id", blankMessageIDAck()},
		{"blank from", blankFromAck()},
		{"blank restatement", blankRestatementAck()},
		{"invalid status", invalidStatusAck()},
		{"correction on pending status", correctionOnPendingAck()},
		{"corrected status with no correction", correctedWithNoCorrectionAck()},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			bus := events.New()
			r := &recorder{}
			if err := bus.Subscribe(agent.MessageAckedEvent, r.handler); err != nil {
				t.Fatalf("Subscribe() unexpected error: %v", err)
			}
			wantErr := tt.ack.Validate()
			if wantErr == nil {
				t.Fatal("ack validated cleanly, want a validation error")
			}

			err := agent.EmitMessageAcked(context.Background(), bus, tt.ack)
			if err == nil {
				t.Fatal("EmitMessageAcked() returned a nil error, want the validation error")
			}
			if err.Error() != wantErr.Error() {
				t.Fatalf("EmitMessageAcked() error = %q, want %q", err.Error(), wantErr.Error())
			}
			if got := r.count(); got != 0 {
				t.Fatalf("handler ran %d times, want 0", got)
			}
		})
	}
}

// TestEmitThreadVerifiedFailureModes proves EmitThreadVerified
// surfaces every VerifyThread rejection branch, unwrapped, and runs
// no handler for any of them. A translator that only checked
// PrevHash linkage, instead of delegating fully to VerifyThread,
// would pass TestEmitThreadVerifiedBrokenChain but fail the cases
// here.
func TestEmitThreadVerifiedFailureModes(t *testing.T) {
	cases := []struct {
		name string
		msgs []envelope.Message
	}{
		{"empty thread", emptyThread()},
		{"duplicate id", duplicateIDThread()},
		{"thread id mismatch", threadIDMismatchThread()},
		{"first message carries prev hash", firstMessagePrevHashThread()},
		{"broken chain", brokenThread()},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			bus := events.New()
			r := &recorder{}
			if err := bus.Subscribe(agent.ThreadVerifiedEvent, r.handler); err != nil {
				t.Fatalf("Subscribe() unexpected error: %v", err)
			}
			wantErr := envelope.VerifyThread(tt.msgs)
			if wantErr == nil {
				t.Fatal("thread verified cleanly, want a verification error")
			}

			err := agent.EmitThreadVerified(context.Background(), bus, tt.msgs)
			if err == nil {
				t.Fatal("EmitThreadVerified() returned a nil error, want the verification error")
			}
			if err.Error() != wantErr.Error() {
				t.Fatalf("EmitThreadVerified() error = %q, want %q", err.Error(), wantErr.Error())
			}
			if got := r.count(); got != 0 {
				t.Fatalf("handler ran %d times, want 0", got)
			}
		})
	}
}
