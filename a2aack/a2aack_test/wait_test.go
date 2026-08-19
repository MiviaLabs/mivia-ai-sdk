package a2aack_test

import (
	"context"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/a2aack"
	"github.com/MiviaLabs/mivia-ai-sdk/a2aclient"
	"github.com/MiviaLabs/mivia-ai-sdk/a2aloopback"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// TestWaitLiveLoopback runs the real a2aclient.Client through
// a2aloopback.Loopback and asserts the resulting ack: MessageID equals
// the sent step message's id, Status is confirmed, and From and the
// restatement come from the server's reply.
func TestWaitLiveLoopback(t *testing.T) {
	addr, stop, err := a2aloopback.Loopback()
	if err != nil {
		t.Fatalf("Loopback() error: %v", err)
	}
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("Loopback stop() error: %v", err)
		}
	})

	client, err := a2aclient.New(addr)
	if err != nil {
		t.Fatalf("a2aclient.New() error: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("client.Close() error: %v", err)
		}
	})

	opts := a2aack.Options{Poll: 2 * time.Millisecond, Timeout: time.Second}
	ackFn, err := a2aack.Wait(client, opts)
	if err != nil {
		t.Fatalf("Wait() returned validation error %v", err)
	}

	msg := signedMessage(t)
	ack, err := ackFn(context.Background(), msg)
	if err != nil {
		t.Fatalf("ackFn() error: %v", err)
	}
	if ack.MessageID != msg.ID {
		t.Fatalf("ack.MessageID = %q, want the sent message id %q", ack.MessageID, msg.ID)
	}
	if ack.Status != envelope.AckConfirmed {
		t.Fatalf("ack.Status = %q, want confirmed", ack.Status)
	}
	if ack.Restatement != msg.Payload {
		t.Fatalf("ack.Restatement = %q, want the echoed payload %q", ack.Restatement, msg.Payload)
	}
	if ack.From == "" {
		t.Fatal("ack.From is empty, want the server's signer")
	}
	if ack.From == msg.Signer {
		t.Fatal("ack.From equals the sender's signer, want the remote agent's own key")
	}
}
