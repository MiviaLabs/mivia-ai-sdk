package a2aclient

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/a2aclient/a2atest"
)

// TestNewWithTLSRejectsBadInput pins the constructor's validation: an
// empty baseURL fails ErrNoBaseURL and a nil cfg fails ErrNoTLSConfig.
// NewWithTLS never guesses a dial mode.
func TestNewWithTLSRejectsBadInput(t *testing.T) {
	if _, err := NewWithTLS("", &tls.Config{}); !errors.Is(err, ErrNoBaseURL) {
		t.Fatalf("empty baseURL error = %v, want errors.Is ErrNoBaseURL", err)
	}
	if _, err := NewWithTLS("bufnet", nil); !errors.Is(err, ErrNoTLSConfig) {
		t.Fatalf("nil cfg error = %v, want errors.Is ErrNoTLSConfig", err)
	}
}

// TestNewWithTLSOpensLazyTransport proves the dial stays lazy: a
// non-nil config against an unresolvable address constructs and
// closes, with no synchronous connect.
func TestNewWithTLSOpensLazyTransport(t *testing.T) {
	c, err := NewWithTLS("dns:///agent.example.invalid:443", &tls.Config{})
	if err != nil {
		t.Fatalf("NewWithTLS: %v", err)
	}
	if c == nil {
		t.Fatal("NewWithTLS returned a nil Client on success")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestNewLiveLoopbackRoundTrip drives the full sign, send, poll,
// verify round trip through New(addr) against the plaintext loopback.
// This is the live TLS-free path test. a2atest.Loopback serves no
// TLS, so a TLS handshake stays untested; a TLS-serving fixture is
// its own future scope.
func TestNewLiveLoopbackRoundTrip(t *testing.T) {
	addr, stop, err := a2atest.Loopback()
	if err != nil {
		t.Fatalf("Loopback: %v", err)
	}
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("stop: %v", err)
		}
	})
	c, err := New(addr)
	if err != nil {
		t.Fatalf("New: %v", err)
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
