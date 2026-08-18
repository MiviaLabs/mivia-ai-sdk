// Package agent defines one agent declaratively: an identity, a
// capability card, and a step plan, bound into one value. It also
// holds the envelope-to-events translator, the composition-layer
// code that turns a delivered Message, an Ack, or a verified thread
// into one events.Event.
//
// Map: agent.go = Agent, New, Name, Capabilities. events.go =
// MessageDeliveredEvent, MessageAckedEvent, ThreadVerifiedEvent.
// translator.go = ErrNoBus, EmitMessageDelivered, EmitMessageAcked,
// EmitThreadVerified. run.go = AckWait, Run, ErrEscalated, ErrNoWait,
// ErrNoThread, ErrOverBudget, Run's optional *heartbeat.Monitor beat
// and forget logic, Run's optional *contextbudget.Limits budget
// check, and confirmStep's room-stamping of Message.Room before
// a.id.Sign. The definition is data; it states who the agent is,
// what it can do, and what it runs. Run drives the bound plan
// in-process, through flow.Run. Rationale: ../docs/plans/agent.md.
// Contribution rules: ../AGENTS.md.
package agent
