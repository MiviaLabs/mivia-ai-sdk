// Package agent defines one agent declaratively: an identity, a
// capability card, and a step plan, bound into one value.
//
// Map: agent.go = Agent, New, Name, Capabilities. The definition is
// data; it states who the agent is, what it can do, and what it
// runs. It does not run yet; the execution loop lands in a later
// phase. Rationale: ../docs/plans/agent.md. Contribution rules:
// ../AGENTS.md.
package agent
