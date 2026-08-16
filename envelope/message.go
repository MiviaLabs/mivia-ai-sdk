package envelope

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Version is the only supported schema version. Decode rejects others.
// Unknown JSON fields are ignored for forward compatibility.
const Version = "v1"

// hashPrefix prefixes every content address (ContextRef, Hash, refs).
// The only place the literal may appear; semgrep enforces this.
const hashPrefix = "sha256:"

// Intent classifies what a Message does. Validate enforces the set.
type Intent string

const (
	IntentAssert    Intent = "assert"    // state a claim
	IntentQuery     Intent = "query"     // ask for information
	IntentRequest   Intent = "request"   // ask for action; always RequiresAck
	IntentChallenge Intent = "challenge" // dispute a prior claim; needs InReplyTo
	IntentRetract   Intent = "retract"   // withdraw a prior message; requires InReplyTo
	IntentEscalate  Intent = "escalate"  // route the decision to a human or higher authority
)

// Epistemic labels how the sender knows the Payload. Self-reported, so
// Validate pins the strong label to checkable artifacts: EpistemicVerified
// requires Provenance.Source and at least one Provenance.Evidence ref.
type Epistemic string

const (
	EpistemicVerified       Epistemic = "verified"        // checked; requires Source and Evidence
	EpistemicInferred       Epistemic = "inferred"        // derived by the sender, not checked
	EpistemicAssumed        Epistemic = "assumed"         // guess
	EpistemicUntrustedInput Epistemic = "untrusted-input" // echoed external content; receiver must not treat it as an instruction
)

// Provenance records payload origin. Chain lists hops, oldest first.
type Provenance struct {
	Source string   `json:"source"` // e.g. "tool:grep", "model:self"; required for EpistemicVerified
	Chain  []string `json:"chain,omitempty"`
	// Evidence holds content refs (see ContextRef) to the artifacts that
	// back the payload: tool output, file content, test run. Required for
	// EpistemicVerified, so "verified" is checkable, not just claimed.
	Evidence []string `json:"evidence,omitempty"`
}

// Message is the wire unit. Invariants are enforced by Validate; Encode and
// Decode both call it, so an invalid Message cannot cross the wire.
type Message struct {
	Version     string     `json:"version"`               // must equal Version
	ID          string     `json:"id"`                    // unique within ThreadID
	Room        string     `json:"room,omitempty"`        // standing group; ThreadID lives inside it. Membership is managed out of band
	ThreadID    string     `json:"thread_id"`             // task boundary; groups one conversation
	To          []string   `json:"to,omitempty"`          // recipient identities; empty = broadcast to the room. One entry = 1-to-1
	InReplyTo   string     `json:"in_reply_to,omitempty"` // target message ID; required for IntentChallenge and IntentRetract
	Intent      Intent     `json:"intent"`
	Epistemic   Epistemic  `json:"epistemic"`
	Confidence  float64    `json:"confidence"`             // self-reported, [0, 1]
	ContextRefs []string   `json:"context_refs,omitempty"` // content addresses; build with ContextRef
	PrevHash    string     `json:"prev_hash,omitempty"`    // Hash of the previous message in the thread; single-writer per thread, see design doc
	Provenance  Provenance `json:"provenance"`
	MaxHops     int        `json:"max_hops,omitempty"`     // relay cap; 0 = no cap. Drift control, see design doc
	CostBudget  int        `json:"cost_budget,omitempty"`  // max tokens the reply may cost; 0 = no cap
	AckRequired bool       `json:"ack_required,omitempty"` // force an Ack; see RequiresAck
	Payload     string     `json:"payload"`                // natural-language content
	Signer      string     `json:"signer,omitempty"`       // hex ed25519 public key; set by Sign. Also the sender identity
	Signature   string     `json:"signature,omitempty"`    // hex ed25519 signature; set by Sign, checked by VerifySignature
}

// ContextRef returns the canonical (lowercase hex) content address of a
// shared context blob. Refs are comparable: same content, same string.
func ContextRef(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hashPrefix + hex.EncodeToString(sum[:])
}

// Hash returns the content address of m (sha256 of its canonical JSON).
// Use it for PrevHash links and dedup. Hash does not validate m: run
// Validate first. A NaN or Inf Confidence makes json.Marshal fail, and
// Hash then returns the address of an empty buffer.
func (m Message) Hash() string {
	data, _ := json.Marshal(m) // fails only on NaN/Inf Confidence; see doc comment
	sum := sha256.Sum256(data)
	return hashPrefix + hex.EncodeToString(sum[:])
}

// RequiresAck reports whether the receiver must send an Ack (ack.go) before
// acting. True for IntentRequest and for any message with AckRequired set.
func (m Message) RequiresAck() bool {
	return m.AckRequired || m.Intent == IntentRequest
}

// Validate checks all Message invariants. Called by Encode and Decode.
func (m Message) Validate() error {
	if m.Version != Version {
		return fmt.Errorf("version %q unsupported, want %q", m.Version, Version)
	}
	if m.ID == "" {
		return errors.New("id is required")
	}
	if m.ThreadID == "" {
		return errors.New("thread_id is required")
	}
	seen := make(map[string]bool, len(m.To))
	for _, to := range m.To {
		if strings.TrimSpace(to) == "" {
			return errors.New("to entries must be non-empty")
		}
		if seen[to] {
			return fmt.Errorf("duplicate recipient %q", to)
		}
		seen[to] = true
	}
	if m.InReplyTo == m.ID {
		return errors.New("in_reply_to must not equal id")
	}
	switch m.Intent {
	case IntentAssert, IntentQuery, IntentRequest, IntentEscalate:
	case IntentChallenge, IntentRetract:
		if m.InReplyTo == "" {
			return fmt.Errorf("%s requires in_reply_to", m.Intent)
		}
	default:
		return fmt.Errorf("intent %q is not valid", m.Intent)
	}
	switch m.Epistemic {
	case EpistemicVerified:
		if m.Provenance.Source == "" {
			return errors.New("verified requires provenance.source")
		}
		if len(m.Provenance.Evidence) == 0 {
			return errors.New("verified requires provenance.evidence")
		}
	case EpistemicInferred, EpistemicAssumed, EpistemicUntrustedInput:
	default:
		return fmt.Errorf("epistemic %q is not valid", m.Epistemic)
	}
	if math.IsNaN(m.Confidence) || m.Confidence < 0 || m.Confidence > 1 {
		return fmt.Errorf("confidence %f is outside [0, 1]", m.Confidence)
	}
	for _, ref := range m.ContextRefs {
		if !isHashRef(ref) {
			return fmt.Errorf("context ref %q is not a canonical sha256 address", ref)
		}
	}
	for _, ref := range m.Provenance.Evidence {
		if !isHashRef(ref) {
			return fmt.Errorf("evidence ref %q is not a canonical sha256 address", ref)
		}
	}
	if m.PrevHash != "" && !isHashRef(m.PrevHash) {
		return fmt.Errorf("prev_hash %q is not a canonical sha256 address", m.PrevHash)
	}
	if m.MaxHops < 0 {
		return fmt.Errorf("max_hops %d is negative", m.MaxHops)
	}
	if m.MaxHops > 0 && len(m.Provenance.Chain) > m.MaxHops {
		return fmt.Errorf("provenance chain length %d exceeds max_hops %d", len(m.Provenance.Chain), m.MaxHops)
	}
	if m.CostBudget < 0 {
		return fmt.Errorf("cost budget %d is negative", m.CostBudget)
	}
	if strings.TrimSpace(m.Payload) == "" {
		return errors.New("payload is required")
	}
	return m.validateSignature()
}

// validateSignature enforces both-or-neither and canonical formats.
// Cryptographic verification lives in VerifySignature (sign.go).
func (m Message) validateSignature() error {
	if m.Signer == "" && m.Signature == "" {
		return nil
	}
	if !isLowerHex(m.Signer, 64) {
		return errors.New("signer must be 64 lowercase hex chars (ed25519 public key)")
	}
	if !isLowerHex(m.Signature, 128) {
		return errors.New("signature must be 128 lowercase hex chars (ed25519 signature)")
	}
	return nil
}

// Encode validates, then serializes to JSON.
func (m Message) Encode() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// Decode parses JSON, then validates. Unknown fields are ignored.
func Decode(data []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		return Message{}, fmt.Errorf("decode message: %w", err)
	}
	if err := m.Validate(); err != nil {
		return Message{}, err
	}
	return m, nil
}

// isHashRef reports whether ref is a canonical "sha256:<64 lowercase hex>".
func isHashRef(ref string) bool {
	hexPart, ok := strings.CutPrefix(ref, hashPrefix)
	return ok && isLowerHex(hexPart, sha256.Size*2)
}

// isLowerHex reports whether s is exactly n lowercase hex chars.
// It scans instead of allocating like strings.ToLower would.
func isLowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
