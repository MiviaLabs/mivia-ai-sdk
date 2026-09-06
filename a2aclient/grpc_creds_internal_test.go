package a2aclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/a2aloopback"
	"google.golang.org/grpc/credentials/insecure"
)

// TestNewWithCredentialsRejectsBadInput pins the constructor's
// validation: an empty baseURL fails ErrNoBaseURL and nil credentials
// fail ErrNoCredentials. NewWithCredentials never guesses a dial mode.
func TestNewWithCredentialsRejectsBadInput(t *testing.T) {
	if _, err := NewWithCredentials("", insecure.NewCredentials()); !errors.Is(err, ErrNoBaseURL) {
		t.Fatalf("empty baseURL error = %v, want ErrNoBaseURL", err)
	}
	if _, err := NewWithCredentials("bufnet", nil); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("nil credentials error = %v, want ErrNoCredentials", err)
	}
}

// TestNewWithCredentialsLiveLoopback dials the live plaintext loopback
// with explicitly supplied credentials and drives one full round trip.
// It proves the caller-supplied credentials reach the dial.
func TestNewWithCredentialsLiveLoopback(t *testing.T) {
	addr, stop, err := a2aloopback.Loopback()
	if err != nil {
		t.Fatalf("Loopback: %v", err)
	}
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("stop: %v", err)
		}
	})
	c, err := NewWithCredentials(addr, insecure.NewCredentials())
	if err != nil {
		t.Fatalf("NewWithCredentials: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	msg := signedMessage(t)
	h, err := c.Send(ctx, msg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	for {
		state, err := c.Status(ctx, h)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if state == StateCompleted || state == StateFailed {
			break
		}
	}
	res, err := c.Result(ctx, h)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if res.Payload != msg.Payload {
		t.Fatalf("Result payload = %q, want the echoed %q", res.Payload, msg.Payload)
	}
}
