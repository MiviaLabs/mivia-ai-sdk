// Package agent defines one agent declaratively: an identity, a
// capability card, and a step plan, bound into one value. It also
// holds the envelope-to-events translator, the composition-layer
// code that turns a delivered Message, an Ack, or a verified thread
// into one events.Event.
//
// Map: agent.go = Agent, New, Name, Capabilities. events.go =
// MessageDeliveredEvent, MessageAckedEvent, ThreadVerifiedEvent.
// translator.go = ErrNoBus, EmitMessageDelivered, EmitMessageAcked,
// EmitThreadVerified. The definition is data; it states who the
// agent is, what it can do, and what it runs. It does not run yet;
// the execution loop lands in a later phase. Rationale:
// ../docs/plans/agent.md. Contribution rules: ../AGENTS.md.
package agent
