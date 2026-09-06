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
	if mapped.Part.Text == "" {
		t.Fatal("Part.Text is empty")
	}
	if len(mapped.Part.Data) != 0 {
		t.Fatal("Part.Data is set, want empty: ToPart fills only Text")
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

// TestToPartRejectsInvalidMessage proves ToPart rejects an invalid
// message and returns an error, not a zero Mapped disguised as
// success, on an empty Payload.
func TestToPartRejectsInvalidMessage(t *testing.T) {
	m := validMessage()
	m.Payload = ""
	_, err := a2a.ToPart(m)
	if err == nil {
		t.Fatal("ToPart accepted a message with an empty payload")
	}
}

// TestToPartInvalidMessageErrorMatchesValidate proves ToPart returns
// the Validate error unwrapped. Encode validates first and returns
// that error as-is; the test fails if a future Encode wraps it.
func TestToPartInvalidMessageErrorMatchesValidate(t *testing.T) {
	m := validMessage()
	m.Payload = ""
	want := m.Validate()
	if want == nil {
		t.Fatal("Validate accepted a message with an empty payload")
	}
	_, err := a2a.ToPart(m)
	if err == nil {
		t.Fatal("ToPart accepted a message with an empty payload")
	}
	if err.Error() != want.Error() {
		t.Fatalf("ToPart error = %q, want %q", err.Error(), want.Error())
	}
}

// TestFromPartRejectsEmptyData pins the Data fallback's Validate
// failure path: with Text empty, a Mapped whose Part.Data is an empty
// JSON object decodes but fails FromPart through Validate (missing id,
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

// TestFromPartRejectsMalformedData pins the Data fallback's decode
// error path: with Text empty, a Mapped whose Part.Data fails to
// unmarshal into envelope.Message fails FromPart before Validate ever
// runs, and returns no Message value. The failure string must be a
// decode error, not a Validate error, to prove Validate never ran.
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

// TestFromPartReadsTextFirst proves Text wins over Data: a Part with
// a valid Text and a different valid Data decodes the Text side, and
// the Data content is ignored.
func TestFromPartReadsTextFirst(t *testing.T) {
	text, err := validMessage().Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	other := validMessage()
	other.Payload = "The data side."
	data, err := other.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	mapped := a2a.Mapped{
		Part:      a2a.Part{Text: string(text), Data: data},
		ContextID: "thread-1",
		MessageID: "msg-1",
	}
	got, err := a2a.FromPart(mapped)
	if err != nil {
		t.Fatalf("FromPart: %v", err)
	}
	if got.Payload != "The build is green." {
		t.Fatalf("Payload = %q, want the Text side's payload, not the Data side's", got.Payload)
	}
}

// TestFromPartRejectsMalformedText proves a Part whose Text holds a
// malformed JSON value fails FromPart with a decode error and a zero
// Message.
func TestFromPartRejectsMalformedText(t *testing.T) {
	mapped := a2a.Mapped{
		Part:      a2a.Part{Text: "{not json"},
		ContextID: "thread-1",
		MessageID: "msg-1",
	}
	got, err := a2a.FromPart(mapped)
	if err == nil {
		t.Fatal("FromPart accepted malformed text")
	}
	if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error looks like a Validate error, want a decode error: %v", err)
	}
	if !reflect.DeepEqual(got, envelope.Message{}) {
		t.Fatalf("FromPart returned a non-zero Message on decode failure: %+v", got)
	}
}

// TestFromPartRejectsEmptyPart proves an empty Text and an empty Data
// fail the decode and return no Message value.
func TestFromPartRejectsEmptyPart(t *testing.T) {
	mapped := a2a.Mapped{
		Part:      a2a.Part{},
		ContextID: "thread-1",
		MessageID: "msg-1",
	}
	got, err := a2a.FromPart(mapped)
	if err == nil {
		t.Fatal("FromPart accepted a part with no text and no data")
	}
	if !reflect.DeepEqual(got, envelope.Message{}) {
		t.Fatalf("FromPart returned a non-zero Message on an empty part: %+v", got)
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
// prefixed file in testdata/vectors must round-trip: FromPart on the
// fixture part reproduces the fixture message, and FromPart on a
// fresh ToPart mapping of the fixture message reproduces it too.
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

			fresh, err := a2a.FromPart(a2a.Mapped{
				Part:      mapped.Part,
				ContextID: fixture.ContextID,
				MessageID: fixture.MessageID,
			})
			if err != nil {
				t.Fatalf("FromPart on the fresh mapping: %v", err)
			}
			if !reflect.DeepEqual(fresh, fixture.Message) {
				t.Fatalf("fresh mapping round trip = %+v, want %+v", fresh, fixture.Message)
			}
		})
	}
}

// TestTextVectorByteExact proves ToPart of the text vector's message
// reproduces the vector's part text byte for byte: the carrier holds
// the exact Encode bytes.
func TestTextVectorByteExact(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("../testdata/vectors", "valid_mapped_text.json"))
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
	if mapped.Part.Text != fixture.Part.Text {
		t.Fatalf("Part.Text = %s, want %s", mapped.Part.Text, fixture.Part.Text)
	}
}
