// Package dispatch receives newline-delimited envelope JSON over HTTP,
// runs the full receive ladder per line, and answers with
// newline-delimited ack JSON. Send posts messages and collects the
// replies. See docs/plans/dispatch.md for the contract.
//
// The ladder runs, per line, in fixed order and fails fast: Decode,
// VerifySignature, Room.Accepts, resolve, handle, then NewAck, Confirm,
// and Encode build the reply. agent.EmitMessageDelivered and
// agent.EmitMessageAcked are best-effort diagnostics outside this
// ladder, called after their point in the sequence with their error
// return ignored; they never fail a line.
//
// MessageDeliveredEvent fires after VerifySignature and before
// Room.Accepts. It means "signature verified," not "room-admitted": a
// delivered event can still precede an admission rejection.
package dispatch
