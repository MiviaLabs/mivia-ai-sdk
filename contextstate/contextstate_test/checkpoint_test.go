package contextstate_test

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
)

// id128 is an identifier at exactly MaxIdentifierBytes.
var id128 = strings.Repeat("i", contextstate.MaxIdentifierBytes)

// id129 is an identifier one byte over MaxIdentifierBytes.
var id129 = strings.Repeat("i", contextstate.MaxIdentifierBytes+1)

func TestSourceIDValidate(t *testing.T) {
	at := func(session string) contextstate.SourceID {
		return contextstate.SourceID{SessionID: session, Sequence: 1}
	}
	cases := []struct {
		name    string
		id      contextstate.SourceID
		wantErr bool
	}{
		{"valid", at("session-a"), false},
		{"bound at max", at(id128), false},
		{"over max", at(id129), true},
		{"blank", at(""), true},
		{"whitespace only", at("   "), true},
		{"control character", at("session\x01a"), true},
		{"delete character", at("session\x7fa"), true},
		{"invalid utf-8", at("session\xff"), true},
		{"inner space accepted", at("session a"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.id.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate accepted an invalid SourceID")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate rejected a valid SourceID: %v", err)
			}
		})
	}
}

func TestSourceRangeValidate(t *testing.T) {
	span := func(start, end uint64) contextstate.SourceRange {
		return contextstate.SourceRange{
			Start: contextstate.SourceID{SessionID: "session-a", Sequence: start},
			End:   contextstate.SourceID{SessionID: "session-a", Sequence: end},
		}
	}
	cases := []struct {
		name    string
		r       contextstate.SourceRange
		wantErr bool
	}{
		{"valid span", span(1, 10), false},
		{"single event", span(4, 4), false},
		{"span at max minus one", span(0, contextstate.MaxSourceRangeEvents-1), false},
		{"span at max", span(0, contextstate.MaxSourceRangeEvents), true},
		{"span over max", span(0, contextstate.MaxSourceRangeEvents+1), true},
		{"reversed", span(10, 1), true},
		{"cross session", contextstate.SourceRange{
			Start: contextstate.SourceID{SessionID: "session-a", Sequence: 1},
			End:   contextstate.SourceID{SessionID: "session-b", Sequence: 2},
		}, true},
		{"blank start session", contextstate.SourceRange{
			Start: contextstate.SourceID{SessionID: "", Sequence: 1},
			End:   contextstate.SourceID{SessionID: "session-a", Sequence: 2},
		}, true},
		{"blank end session", contextstate.SourceRange{
			Start: contextstate.SourceID{SessionID: "session-a", Sequence: 1},
			End:   contextstate.SourceID{SessionID: "", Sequence: 2},
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.r.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate accepted an invalid SourceRange")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate rejected a valid SourceRange: %v", err)
			}
		})
	}
}

func TestSourceEventValidate(t *testing.T) {
	base := func() contextstate.SourceEvent {
		return contextstate.SourceEvent{
			ID:              contextstate.SourceID{SessionID: "session-a", Sequence: 1},
			Kind:            "message",
			Role:            "user",
			Provenance:      "fixture",
			RedactionStatus: "none",
			Size:            4,
		}
	}
	over256 := strings.Repeat("k", 257)
	at256 := strings.Repeat("k", 256)
	cases := []struct {
		name    string
		mutate  func(*contextstate.SourceEvent)
		wantErr bool
	}{
		{"valid", func(*contextstate.SourceEvent) {}, false},
		{"optional fields set", func(e *contextstate.SourceEvent) {
			e.ToolCallID = "call-1"
			e.PayloadRef = contextstate.Mint([]byte("payload"))
		}, false},
		{"blank kind", func(e *contextstate.SourceEvent) { e.Kind = "" }, true},
		{"blank role", func(e *contextstate.SourceEvent) { e.Role = " " }, true},
		{"blank provenance", func(e *contextstate.SourceEvent) { e.Provenance = "" }, true},
		{"blank redaction status", func(e *contextstate.SourceEvent) { e.RedactionStatus = "" }, true},
		{"kind over 256", func(e *contextstate.SourceEvent) { e.Kind = over256 }, true},
		{"kind at 256", func(e *contextstate.SourceEvent) { e.Kind = at256 }, false},
		{"tool call id over max", func(e *contextstate.SourceEvent) { e.ToolCallID = id129 }, true},
		{"tool call id at max", func(e *contextstate.SourceEvent) { e.ToolCallID = id128 }, false},
		{"payload ref over max", func(e *contextstate.SourceEvent) {
			e.PayloadRef = strings.Repeat("p", contextstate.MaxPayloadReferenceBytes+1)
		}, true},
		{"payload ref at max", func(e *contextstate.SourceEvent) {
			e.PayloadRef = strings.Repeat("p", contextstate.MaxPayloadReferenceBytes)
		}, false},
		{"blank session", func(e *contextstate.SourceEvent) { e.ID.SessionID = "" }, true},
		{"negative size", func(e *contextstate.SourceEvent) { e.Size = -1 }, true},
		{"zero size", func(e *contextstate.SourceEvent) { e.Size = 0 }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := base()
			tc.mutate(&event)
			err := event.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate accepted an invalid SourceEvent")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate rejected a valid SourceEvent: %v", err)
			}
		})
	}
}

func TestBindingRevisionValidate(t *testing.T) {
	base := func() contextstate.BindingRevision {
		return contextstate.BindingRevision{Provider: "provider-a", Model: "model-b", Generation: 1}
	}
	cases := []struct {
		name    string
		mutate  func(*contextstate.BindingRevision)
		wantErr bool
	}{
		{"valid", func(*contextstate.BindingRevision) {}, false},
		{"blank provider", func(b *contextstate.BindingRevision) { b.Provider = "" }, true},
		{"blank model", func(b *contextstate.BindingRevision) { b.Model = "" }, true},
		{"provider over max", func(b *contextstate.BindingRevision) { b.Provider = id129 }, true},
		{"model over max", func(b *contextstate.BindingRevision) { b.Model = id129 }, true},
		{"zero generation", func(b *contextstate.BindingRevision) { b.Generation = 0 }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			binding := base()
			tc.mutate(&binding)
			err := binding.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate accepted an invalid BindingRevision")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate rejected a valid BindingRevision: %v", err)
			}
		})
	}
}

func fixtureCheckpointID() contextstate.CheckpointID {
	return contextstate.CheckpointID{
		SessionID: "session-a",
		SourceRange: contextstate.SourceRange{
			Start: contextstate.SourceID{SessionID: "session-a", Sequence: 1},
			End:   contextstate.SourceID{SessionID: "session-a", Sequence: 2},
		},
		Algorithm:      "fixture-algorithm",
		SchemaVersion:  1,
		IdempotencyKey: "op-1",
	}
}

func TestCheckpointIDValidate(t *testing.T) {
	at64 := strings.Repeat("a", 64)
	cases := []struct {
		name    string
		mutate  func(*contextstate.CheckpointID)
		wantErr bool
	}{
		{"valid", func(*contextstate.CheckpointID) {}, false},
		{"blank session", func(id *contextstate.CheckpointID) { id.SessionID = "" }, true},
		{"range from another session", func(id *contextstate.CheckpointID) {
			id.SourceRange.Start.SessionID = "session-b"
			id.SourceRange.End.SessionID = "session-b"
		}, true},
		{"blank algorithm", func(id *contextstate.CheckpointID) { id.Algorithm = "" }, true},
		{"algorithm at 64", func(id *contextstate.CheckpointID) { id.Algorithm = at64 }, false},
		{"algorithm over 64", func(id *contextstate.CheckpointID) { id.Algorithm = at64 + "a" }, true},
		{"zero schema version", func(id *contextstate.CheckpointID) { id.SchemaVersion = 0 }, true},
		{"blank idempotency key", func(id *contextstate.CheckpointID) { id.IdempotencyKey = "" }, true},
		{"key over max", func(id *contextstate.CheckpointID) { id.IdempotencyKey = id129 }, true},
		{"invalid range", func(id *contextstate.CheckpointID) {
			id.SourceRange.End.Sequence = 0
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := fixtureCheckpointID()
			tc.mutate(&id)
			err := id.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate accepted an invalid CheckpointID")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate rejected a valid CheckpointID: %v", err)
			}
		})
	}
}

func fixtureCheckpoint() contextstate.Checkpoint {
	return contextstate.Checkpoint{
		ID:            fixtureCheckpointID(),
		Revision:      contextstate.Revision{Session: 1, Durable: 1, Source: 2},
		Binding:       contextstate.BindingRevision{Provider: "provider-a", Model: "model-b", Generation: 1},
		ActiveContext: []byte("fixture-context"),
		TurnID:        7,
	}
}

func TestCheckpointValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*contextstate.Checkpoint)
		wantErr bool
	}{
		{"valid", func(*contextstate.Checkpoint) {}, false},
		{"empty active context", func(c *contextstate.Checkpoint) { c.ActiveContext = nil }, true},
		{"zero turn", func(c *contextstate.Checkpoint) { c.TurnID = 0 }, true},
		{"invalid id", func(c *contextstate.Checkpoint) { c.ID.SessionID = "" }, true},
		{"invalid binding", func(c *contextstate.Checkpoint) { c.Binding.Provider = "" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkpoint := fixtureCheckpoint()
			tc.mutate(&checkpoint)
			err := checkpoint.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate accepted an invalid Checkpoint")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate rejected a valid Checkpoint: %v", err)
			}
		})
	}
}
