package a2a_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// validMessage builds a minimal message that passes envelope.Validate.
func validMessage() envelope.Message {
	return envelope.Message{
		Version:    envelope.Version,
		ID:         "msg-1",
		ThreadID:   "thread-1",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicInferred,
		Confidence: 0.5,
		Provenance: envelope.Provenance{Source: "model:self"},
		Payload:    "The build is green.",
	}
}

// TestToPartRoundTrip proves a minimal valid message round-trips
// through ToPart and FromPart, with ID and ThreadID surviving as
// Mapped.MessageID and Mapped.ContextID.
func TestToPartRoundTrip(t *testing.T) {
	m := validMessage()
	mapped, err := a2a.ToPart(m)
	if err != nil {
		t.Fatalf("ToPart: %v", err)
	}
	if mapped.ContextID != m.ThreadID {
		t.Fatalf("ContextID = %q, want %q", mapped.ContextID, m.ThreadID)
	}
	if mapped.MessageID != m.ID {
		t.Fatalf("MessageID = %q, want %q", mapped.MessageID, m.ID)
	}
	if len(mapped.Part.Data) == 0 {
		t.Fatal("Part.Data is empty")
	}

	got, err := a2a.FromPart(mapped)
	if err != nil {
		t.Fatalf("FromPart: %v", err)
	}
	if got.ID != m.ID {
		t.Fatalf("ID = %q, want %q", got.ID, m.ID)
	}
	if got.ThreadID != m.ThreadID {
		t.Fatalf("ThreadID = %q, want %q", got.ThreadID, m.ThreadID)
	}
	if got.Payload != m.Payload {
		t.Fatalf("Payload = %q, want %q", got.Payload, m.Payload)
	}
}

// TestToPartRejectsInvalidMessage proves ToPart calls Validate first
// and returns an error, not a zero Mapped disguised as success, on an
// empty Payload.
func TestToPartRejectsInvalidMessage(t *testing.T) {
	m := validMessage()
	m.Payload = ""
	_, err := a2a.ToPart(m)
	if err == nil {
		t.Fatal("ToPart accepted a message with an empty payload")
	}
}

// TestFromPartRejectsEmptyData proves a Mapped whose Part.Data is an
// empty JSON object fails FromPart through Validate (missing id,
// thread_id fields are overwritten by Mapped, but payload stays
// empty).
func TestFromPartRejectsEmptyData(t *testing.T) {
	mapped := a2a.Mapped{
		Part:      a2a.Part{Data: json.RawMessage(`{}`)},
		ContextID: "thread-1",
		MessageID: "msg-1",
	}
	_, err := a2a.FromPart(mapped)
	if err == nil {
		t.Fatal("FromPart accepted an empty data object")
	}
}

// TestFromPartRejectsMalformedData proves a Mapped whose Part.Data
// fails to unmarshal into envelope.Message fails FromPart before
// Validate ever runs, and returns no Message value. The failure
// string must be a decode error, not a Validate error, to prove
// Validate never ran.
func TestFromPartRejectsMalformedData(t *testing.T) {
	mapped := a2a.Mapped{
		Part:      a2a.Part{Data: json.RawMessage(`{"confidence":"not-a-number"}`)},
		ContextID: "thread-1",
		MessageID: "msg-1",
	}
	got, err := a2a.FromPart(mapped)
	if err == nil {
		t.Fatal("FromPart accepted malformed data")
	}
	if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error looks like a Validate error, want a decode error: %v", err)
	}
	if !reflect.DeepEqual(got, envelope.Message{}) {
		t.Fatalf("FromPart returned a non-zero Message on decode failure: %+v", got)
	}
}

// TestFromPartRejectsInvalidMessage proves a Mapped whose Part.Data
// decodes to a Message that fails Validate (empty payload) fails
// FromPart and returns no Message value. This is distinct from the
// decode-failure case: the JSON here parses cleanly.
func TestFromPartRejectsInvalidMessage(t *testing.T) {
	data, err := json.Marshal(envelope.Message{
		Version:    envelope.Version,
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicInferred,
		Confidence: 0.5,
		Provenance: envelope.Provenance{Source: "model:self"},
		Payload:    "",
	})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	mapped := a2a.Mapped{
		Part:      a2a.Part{Data: data},
		ContextID: "thread-1",
		MessageID: "msg-1",
	}
	got, err := a2a.FromPart(mapped)
	if err == nil {
		t.Fatal("FromPart accepted a message that fails Validate")
	}
	if !reflect.DeepEqual(got, envelope.Message{}) {
		t.Fatalf("FromPart returned a non-zero Message on Validate failure: %+v", got)
	}
}

// TestFromPartOverridesEmbeddedIDs proves Mapped.ContextID and
// Mapped.MessageID win over thread_id and id values already embedded
// in Part.Data.
func TestFromPartOverridesEmbeddedIDs(t *testing.T) {
	embedded := validMessage()
	embedded.ID = "embedded-id"
	embedded.ThreadID = "embedded-thread"
	data, err := embedded.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	mapped := a2a.Mapped{
		Part:      a2a.Part{Data: data},
		ContextID: "mapped-thread",
		MessageID: "mapped-id",
	}
	got, err := a2a.FromPart(mapped)
	if err != nil {
		t.Fatalf("FromPart: %v", err)
	}
	if got.ID != "mapped-id" {
		t.Fatalf("ID = %q, want the Mapped.MessageID override", got.ID)
	}
	if got.ThreadID != "mapped-thread" {
		t.Fatalf("ThreadID = %q, want the Mapped.ContextID override", got.ThreadID)
	}
}

// TestFromPartOverrideOrderPrecedesValidate proves FromPart applies
// the Mapped.ContextID/Mapped.MessageID override before it calls
// Validate. Part.Data holds a message that is fully valid on its own,
// with non-empty embedded ThreadID and ID, but Mapped.ContextID and
// Mapped.MessageID are empty. If Validate ran before the override, the
// embedded IDs would let it pass, and FromPart would return the
// message unchanged with no error.
func TestFromPartOverrideOrderPrecedesValidate(t *testing.T) {
	embedded := validMessage()
	data, err := embedded.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	mapped := a2a.Mapped{
		Part:      a2a.Part{Data: data},
		ContextID: "",
		MessageID: "",
	}
	got, err := a2a.FromPart(mapped)
	if err == nil {
		t.Fatalf("FromPart accepted an empty ContextID/MessageID override, got %+v", got)
	}
}

// vectorFixture mirrors a2a/testdata/vectors' JSON shape: the source
// envelope.Message and its mapped Part side by side, with ContextID
// and MessageID as sibling fields outside the part object.
type vectorFixture struct {
	Message   envelope.Message `json:"message"`
	Part      a2a.Part         `json:"part"`
	ContextID string           `json:"context_id"`
	MessageID string           `json:"message_id"`
}

// TestConformanceVectors pins the a2a wire mapping. Every valid_
// prefixed file in testdata/vectors must round-trip: ToPart(message)
// reproduces part/context_id/message_id, and FromPart on the fixture
// reproduces message.
func TestConformanceVectors(t *testing.T) {
	entries, err := os.ReadDir("../testdata/vectors")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		if !strings.HasPrefix(name, "valid_") {
			t.Fatalf("vector name must start with valid_: %s", name)
		}
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../testdata/vectors", name))
			if err != nil {
				t.Fatalf("read vector: %v", err)
			}
			var fixture vectorFixture
			if err := json.Unmarshal(data, &fixture); err != nil {
				t.Fatalf("unmarshal vector: %v", err)
			}

			mapped, err := a2a.ToPart(fixture.Message)
			if err != nil {
				t.Fatalf("ToPart: %v", err)
			}
			if mapped.ContextID != fixture.ContextID {
				t.Fatalf("ContextID = %q, want %q", mapped.ContextID, fixture.ContextID)
			}
			if mapped.MessageID != fixture.MessageID {
				t.Fatalf("MessageID = %q, want %q", mapped.MessageID, fixture.MessageID)
			}
			if string(mapped.Part.Data) != string(fixture.Part.Data) {
				t.Fatalf("Part.Data = %s, want %s", mapped.Part.Data, fixture.Part.Data)
			}

			got, err := a2a.FromPart(a2a.Mapped{
				Part:      fixture.Part,
				ContextID: fixture.ContextID,
				MessageID: fixture.MessageID,
			})
			if err != nil {
				t.Fatalf("FromPart: %v", err)
			}
			if !reflect.DeepEqual(got, fixture.Message) {
				t.Fatalf("round trip = %+v, want %+v", got, fixture.Message)
			}
		})
	}
}
