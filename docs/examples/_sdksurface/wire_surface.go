package main

import (
	"bytes"
	"fmt"
	"io"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/workflow/run"
)

// runSurface round-trips the run artifact store through its wire
// encoding, the way a checkpoint store persists step results.
func runSurface() error {
	artifacts := &run.Artifacts{}
	artifacts.Set("review", "done")
	data, err := artifacts.Encode()
	if err != nil {
		return fmt.Errorf("Encode: %w", err)
	}
	decoded, err := run.DecodeArtifacts(data)
	if err != nil {
		return fmt.Errorf("DecodeArtifacts: %w", err)
	}
	if v, ok := decoded.Get("review"); !ok || v != "done" {
		return fmt.Errorf("decoded review = %q, %v; want done, true", v, ok)
	}
	return nil
}

// channelSurface builds the NDJSON notifier a stdio channel speaks.
func channelSurface() error {
	notif := channel.NewNDJSONNotifier(bytes.NewReader(nil), io.Discard)
	if notif == nil {
		return fmt.Errorf("NewNDJSONNotifier returned nil")
	}
	return nil
}

// planSurface calls the rough token estimate a caller uses to size a
// window before calibration.
func planSurface() error {
	if n := plan.TokenEstimate(0); n != 0 {
		return fmt.Errorf("TokenEstimate(0) = %d, want 0", n)
	}
	return nil
}

// envelopeSurface drives the ack correction chain, the ack request
// probe, and the content reference minter.
func envelopeSurface() error {
	ack := envelope.Ack{}
	corrected := ack.Correct("revised the payload")
	if corrected.Correction == "" {
		return fmt.Errorf("Correct lost the correction text")
	}
	msg := envelope.Message{}
	if msg.RequiresAck() {
		return fmt.Errorf("RequiresAck = true on a plain message")
	}
	if envelope.ContextRef("state") == "" {
		return fmt.Errorf("ContextRef minted an empty reference")
	}
	return nil
}
