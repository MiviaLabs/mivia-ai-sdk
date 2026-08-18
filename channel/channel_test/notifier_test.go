package channel_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
)

// TestNotifierPlainClosure proves Notifier is callable as a plain
// closure with no adapter type: a stub that echoes q.ID into
// Answer.QuestionID and returns Approved: true.
func TestNotifierPlainClosure(t *testing.T) {
	var n channel.Notifier = func(ctx context.Context, q channel.Question) (channel.Answer, error) {
		return channel.Answer{QuestionID: q.ID, Approved: true}, nil
	}

	got, err := n(context.Background(), channel.Question{ID: "q1", Recipient: "human", Payload: "hi"})
	if err != nil {
		t.Fatalf("n() error = %v, want nil", err)
	}
	if got.QuestionID != "q1" {
		t.Fatalf("QuestionID = %q, want %q", got.QuestionID, "q1")
	}
	if !got.Approved {
		t.Fatalf("Approved = false, want true")
	}
}

// TestNotifierError proves a Notifier that returns a non-nil error
// leaves the zero-value Answer untouched.
func TestNotifierError(t *testing.T) {
	wantErr := errors.New("notifier: boom")
	var n channel.Notifier = func(ctx context.Context, q channel.Question) (channel.Answer, error) {
		return channel.Answer{}, wantErr
	}

	got, err := n(context.Background(), channel.Question{ID: "q1", Recipient: "human", Payload: "hi"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("n() error = %v, want %v", err, wantErr)
	}
	if got != (channel.Answer{}) {
		t.Fatalf("Answer = %+v, want zero value", got)
	}
}

// TestNotifierIgnoresContextCancellation proves channel enforces no
// context behavior of its own: a Notifier stub that ignores an
// already-cancelled ctx and still answers normally. Context
// cancellation handling is a Notifier implementation's concern, not
// this package's.
func TestNotifierIgnoresContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var n channel.Notifier = func(ctx context.Context, q channel.Question) (channel.Answer, error) {
		return channel.Answer{QuestionID: q.ID, Approved: false, Payload: "answered anyway"}, nil
	}

	got, err := n(ctx, channel.Question{ID: "q1", Recipient: "human", Payload: "hi"})
	if err != nil {
		t.Fatalf("n() error = %v, want nil", err)
	}
	if got.Payload != "answered anyway" {
		t.Fatalf("Payload = %q, want %q", got.Payload, "answered anyway")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatalf("ctx should be done")
	}
}
