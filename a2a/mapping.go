// Package a2a maps an envelope.Message onto an A2A v1.0 message part
// and back. This phase carries no network call and no third-party
// import. See docs/plans/a2a.md for the contract.
package a2a

import (
	"encoding/json"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// Part is one A2A v1.0 message part. A2A v1.0 has no kind field and
// no separate part classes: one part carries text, raw bytes, a url,
// or a data object. Part carries no message-level field. ContextID
// and MessageID belong to the wrapping A2A Message, not to Part, so
// they live on Mapped, alongside Part, not on Part itself.
type Part struct {
	Text string          `json:"text,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
	Raw  []byte          `json:"raw,omitempty"`
	URL  string          `json:"url,omitempty"`
}

// Mapped is the result of ToPart and the input to FromPart. It holds
// Part plus the two A2A Message-level fields Part cannot carry:
// ContextID (the envelope's ThreadID) and MessageID (the envelope's
// ID).
type Mapped struct {
	Part      Part
	ContextID string
	MessageID string
}

// ToPart maps a signed or unsigned envelope.Message onto a Mapped
// value. It calls m.Validate first and returns an error, not a zero
// Mapped, on failure. It signs nothing and does not modify m. ToPart
// builds Part.Data by calling m.Encode and wrapping the result in
// json.RawMessage, reusing envelope's wire encoder instead of a
// second marshal call; Text, Raw, and URL stay empty. Mapped.ContextID
// carries m.ThreadID and Mapped.MessageID carries m.ID.
func ToPart(m envelope.Message) (Mapped, error) {
	if err := m.Validate(); err != nil {
		return Mapped{}, err
	}
	data, err := m.Encode()
	if err != nil {
		return Mapped{}, err
	}
	return Mapped{
		Part:      Part{Data: json.RawMessage(data)},
		ContextID: m.ThreadID,
		MessageID: m.ID,
	}, nil
}

// FromPart maps a Mapped value back to an envelope.Message. It
// unmarshals mapped.Part.Data into an envelope.Message, then
// overwrites ThreadID with mapped.ContextID and ID with
// mapped.MessageID before calling Validate. FromPart returns an
// error instead of an invalid Message: no malformed part crosses the
// a2a boundary silently.
func FromPart(mapped Mapped) (envelope.Message, error) {
	var m envelope.Message
	if err := json.Unmarshal(mapped.Part.Data, &m); err != nil {
		return envelope.Message{}, err
	}
	m.ThreadID = mapped.ContextID
	m.ID = mapped.MessageID
	if err := m.Validate(); err != nil {
		return envelope.Message{}, err
	}
	return m, nil
}
