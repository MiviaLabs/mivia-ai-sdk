// Package envelope implements the model-to-model message envelope:
// a natural-language payload inside machine-checkable metadata.
//
// Map: message.go = Message, Intent, Epistemic, validation, wire
// (Encode/Decode); ack.go = Ack, the semantic-ack flow; sign.go =
// ed25519 authentication (Sign, VerifySignature).
// Rationale: ../docs/protocol-design.md. Contribution rules: ../AGENTS.md.
package envelope
