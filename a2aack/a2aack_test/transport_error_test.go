package a2aack_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/a2aack"
	"github.com/MiviaLabs/mivia-ai-sdk/a2aclient"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

const errPoll = time.Millisecond
const errTimeout = time.Second

// TestTransportResultError proves a Result error mid-loop propagates
// unwrapped, never wrapped as ErrTimeout or ErrRemoteFailed.
func TestTransportResultError(t *testing.T) {
	fake := &fakeRemote{statusStates: []a2aclient.State{a2aclient.StateCompleted},
		resultErr: errors.New("result fetch failed")}
	msg := signedMessage(t)
	ackFn, err := a2aack.Wait(fake, a2aack.Options{Poll: errPoll, Timeout: errTimeout})
	if err != nil {
		t.Fatalf("Wait returned validation error %v", err)
	}
	_, err = ackFn(context.Background(), msg)
	if err == nil || !strings.Contains(err.Error(), "result fetch failed") {
		t.Fatalf("ackFn() error = %v, want the Result error to propagate", err)
	}
}

// TestTransportSignatureRejected proves an unsigned or tampered result
// fails a2aack's own signature re-verification.
func TestTransportSignatureRejected(t *testing.T) {
	fake := &fakeRemote{statusStates: []a2aclient.State{a2aclient.StateCompleted},
		result: envelope.Message{ID: "res-tampered", Payload: "tampered"}}
	msg := signedMessage(t)
	ackFn, err := a2aack.Wait(fake, a2aack.Options{Poll: errPoll, Timeout: errTimeout})
	if err != nil {
		t.Fatalf("Wait returned validation error %v", err)
	}
	_, err = ackFn(context.Background(), msg)
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("ackFn() error = %q, want a signature-check failure", err)
	}
}

// TestTransportSendErrors proves a non-context Send error propagates
// unwrapped while a context Send error wraps ErrTimeout.
func TestTransportSendErrors(t *testing.T) {
	t.Run("non-context error unwrapped", func(t *testing.T) {
		sendErr := errors.New("transport refused send")
		fake := &fakeRemote{sendErr: sendErr}
		msg := signedMessage(t)
		ackFn, err := a2aack.Wait(fake, a2aack.Options{Poll: errPoll, Timeout: errTimeout})
		if err != nil {
			t.Fatalf("Wait returned validation error %v", err)
		}
		got, err := ackFn(context.Background(), msg)
		if err != sendErr {
			t.Fatalf("ackFn() error = %v (%v), want the Send error unwrapped", err, got)
		}
	})

	t.Run("context error wraps ErrTimeout", func(t *testing.T) {
		fake := &fakeRemote{sendErr: context.DeadlineExceeded}
		msg := signedMessage(t)
		ackFn, err := a2aack.Wait(fake, a2aack.Options{Poll: errPoll, Timeout: errTimeout})
		if err != nil {
			t.Fatalf("Wait returned validation error %v", err)
		}
		_, err = ackFn(context.Background(), msg)
		if !errors.Is(err, a2aack.ErrTimeout) {
			t.Fatalf("ackFn() error = %v, want errors.Is(ErrTimeout)", err)
		}
	})
}

// TestTransportStatusErrors proves a non-context Status error
// propagates unwrapped while a context Status error wraps ErrTimeout.
func TestTransportStatusErrors(t *testing.T) {
	t.Run("non-context error unwrapped", func(t *testing.T) {
		stErr := errors.New("status call failed")
		fake := &fakeRemote{statusErr: stErr}
		msg := signedMessage(t)
		ackFn, err := a2aack.Wait(fake, a2aack.Options{Poll: errPoll, Timeout: errTimeout})
		if err != nil {
			t.Fatalf("Wait returned validation error %v", err)
		}
		got, err := ackFn(context.Background(), msg)
		if err != stErr {
			t.Fatalf("ackFn() error = %v (%v), want the Status error unwrapped", err, got)
		}
	})

	t.Run("context error wraps ErrTimeout", func(t *testing.T) {
		fake := &fakeRemote{statusErr: context.DeadlineExceeded}
		msg := signedMessage(t)
		ackFn, err := a2aack.Wait(fake, a2aack.Options{Poll: errPoll, Timeout: errTimeout})
		if err != nil {
			t.Fatalf("Wait returned validation error %v", err)
		}
		_, err = ackFn(context.Background(), msg)
		if !errors.Is(err, a2aack.ErrTimeout) {
			t.Fatalf("ackFn() error = %v, want errors.Is(ErrTimeout)", err)
		}
	})
}

// TestTransportNewAckRejectsEmptyRestatement proves a signed result
// with an empty payload passes the signature check but its ack
// construction fails on the empty restatement.
func TestTransportNewAckRejectsEmptyRestatement(t *testing.T) {
	fake := &fakeRemote{statusStates: []a2aclient.State{a2aclient.StateCompleted}}
	msg := signedMessage(t)
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signed, err := envelope.Sign(key, envelope.Message{
		Version:    envelope.Version,
		ID:         "res-empty",
		ThreadID:   "thread-1",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicAssumed,
		Confidence: 0.5,
	})
	if err != nil {
		t.Fatalf("sign empty result: %v", err)
	}
	fake.result = signed
	ackFn, err := a2aack.Wait(fake, a2aack.Options{Poll: errPoll, Timeout: errTimeout})
	if err != nil {
		t.Fatalf("Wait returned validation error %v", err)
	}
	_, err = ackFn(context.Background(), msg)
	if err == nil || !strings.Contains(err.Error(), "restatement") {
		t.Fatalf("ackFn() error = %v, want a restatement rejection", err)
	}
}
