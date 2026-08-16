package envelope

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// AckStatus is the state of an Ack. Validate enforces the set.
type AckStatus string

const (
	AckPending   AckStatus = "pending"   // receiver sent the restatement; sender has not ruled
	AckConfirmed AckStatus = "confirmed" // sender accepts the restatement
	AckCorrected AckStatus = "corrected" // sender rejects the restatement; Correction required
)

// Ack is a semantic acknowledgment. Flow: receiver builds it with NewAck
// (pending), sender resolves it with Confirm or Correct. Only a confirmed
// Ack means the receiver may act. In a group thread each recipient sends
// its own Ack; From tells them apart.
type Ack struct {
	MessageID   string    `json:"message_id"` // ID of the acknowledged Message
	From        string    `json:"from"`       // identity of the acking receiver; required
	Restatement string    `json:"restatement"`
	Status      AckStatus `json:"status"`
	Correction  string    `json:"correction,omitempty"` // required when Status is AckCorrected
}

// NewAck builds a pending Ack from receiver identity `from` for msg. The
// sender, not the receiver, sets the final Status; see Confirm, Correct.
func NewAck(msg Message, from, restatement string) (Ack, error) {
	if msg.ID == "" {
		return Ack{}, errors.New("message id is required")
	}
	if strings.TrimSpace(from) == "" {
		return Ack{}, errors.New("from is required")
	}
	if strings.TrimSpace(restatement) == "" {
		return Ack{}, errors.New("restatement is required")
	}
	return Ack{
		MessageID:   msg.ID,
		From:        from,
		Restatement: restatement,
		Status:      AckPending,
	}, nil
}

// Confirm accepts the restatement. Clears any earlier Correction.
func (a Ack) Confirm() Ack {
	a.Status = AckConfirmed
	a.Correction = ""
	return a
}

// Correct rejects the restatement and records the sender's fix.
func (a Ack) Correct(correction string) Ack {
	a.Status = AckCorrected
	a.Correction = correction
	return a
}

// Validate checks all Ack invariants.
func (a Ack) Validate() error {
	if a.MessageID == "" {
		return errors.New("message id is required")
	}
	if strings.TrimSpace(a.From) == "" {
		return errors.New("from is required")
	}
	if strings.TrimSpace(a.Restatement) == "" {
		return errors.New("restatement is required")
	}
	switch a.Status {
	case AckPending, AckConfirmed:
		if a.Correction != "" {
			return fmt.Errorf("correction requires status %q", AckCorrected)
		}
	case AckCorrected:
		if strings.TrimSpace(a.Correction) == "" {
			return errors.New("correction is required when status is corrected")
		}
	default:
		return fmt.Errorf("status %q is not valid", a.Status)
	}
	return nil
}

// Encode validates, then serializes the ack to JSON. Wire counterpart of
// Message.Encode (message.go).
func (a Ack) Encode() ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(a)
}

// DecodeAck parses JSON, then validates. Unknown fields are ignored.
func DecodeAck(data []byte) (Ack, error) {
	var a Ack
	if err := json.Unmarshal(data, &a); err != nil {
		return Ack{}, fmt.Errorf("decode ack: %w", err)
	}
	if err := a.Validate(); err != nil {
		return Ack{}, err
	}
	return a, nil
}
