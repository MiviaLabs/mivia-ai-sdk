// Package a2a maps an envelope.Message onto an A2A v1.0 message part
// and back, and carries the A2A v1.0 client over that mapping. ToPart
// and FromPart hold the mapping. Client sends a message to a remote
// agent and polls task status and results, through the
// a2aproject/a2a-go client. Wait turns one remote round trip into an
// agent step ack. See docs/history/a2a.md for the contract.
package a2a

import (
	"encoding/json"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// Part is one A2A v1.0 message part. A2A v1.0 has no kind field and
// no separate part classes: one part carries text or a data object.
// Part carries no message-level field. ContextID and MessageID belong
// to the wrapping A2A Message, not to Part, so they live on Mapped,
// alongside Part, not on Part itself.
type Part struct {
	Text string          `json:"text,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
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
// value. It returns an error, not a zero Mapped, on failure: m.Encode
// validates m before it marshals, so an invalid message never reaches
// Part.Text. It signs nothing and does not modify m. ToPart sets
// Part.Text to the exact bytes m.Encode returns; Data stays empty.
// Mapped.ContextID carries m.ThreadID and Mapped.MessageID carries
// m.ID.
func ToPart(m envelope.Message) (Mapped, error) {
	data, err := m.Encode()
	if err != nil {
		return Mapped{}, err
	}
	return Mapped{
		Part:      Part{Text: string(data)},
		ContextID: m.ThreadID,
		MessageID: m.ID,
	}, nil
}

// FromPart maps a Mapped value back to an envelope.Message. It
// unmarshals mapped.Part.Text first; an empty Text decodes
// mapped.Part.Data instead, so an old peer's data part still maps
// until v0.4.0. Text wins over Data when both are set; both empty
// fails the decode. FromPart then overwrites ThreadID with
// mapped.ContextID and ID with mapped.MessageID before calling
// Validate. It returns an error instead of an invalid Message: no
// malformed part crosses the a2a boundary silently.
func FromPart(mapped Mapped) (envelope.Message, error) {
	var m envelope.Message
	text := mapped.Part.Text
	if text == "" {
		text = string(mapped.Part.Data)
	}
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		return envelope.Message{}, err
	}
	m.ThreadID = mapped.ContextID
	m.ID = mapped.MessageID
	if err := m.Validate(); err != nil {
		return envelope.Message{}, err
	}
	return m, nil
}
